package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/c-bata/go-prompt"
)

type RequestContext struct {
	url    *url.URL
	client *http.Client
}

var ctx *RequestContext

var oldPath = "/"

var pathCache string
var pathKeys []prompt.Suggest

var fileCache string
var fileKeys []prompt.Suggest

// See https://github.com/eliangcs/http-prompt/blob/master/http_prompt/completion.py
var command = []prompt.Suggest{
	// Command
	{"cd", "Change URL/path"},
	{"pwd", "Show current URL/path"},
	{"exit", "Exit http-prompt"},

	// HTTP Method
	{"delete", "DELETE request"},
	{"get", "GET request"},
	{"patch", "GET request"},
	{"post", "POST request"},
	{"put", "PUT request"},

	// Flag
	{"-v", "set value"},
	{"-f", "set value from file"},
}

func livePrefix() (string, bool) {
	if ctx.url.Path == "/" {
		return "", false
	}
	return ctx.url.String() + "> ", true
}

func split(line string) []string {
	var (
		quoted   bool
		prevRune rune
	)
	return strings.FieldsFunc(line, func(r rune) bool {
		if r == '"' && prevRune != '\\' {
			quoted = !quoted
		}
		return !quoted && unicode.IsSpace(r)
	})
}

func executor(in string) {
	in = strings.TrimSpace(in)

	var (
		method string
		body   io.Reader
	)
	parts := split(in)
	switch parts[0] {
	case "pwd":
		fmt.Println(ctx.url.Path)
		return
	case "exit":
		fmt.Println("Bye!")
		os.Exit(0)
	case "cd":
		pathCache = ""
		pathKeys = nil
		if len(parts) >= 2 && parts[1] == "-" {
			ctx.url.Path, oldPath = oldPath, ctx.url.Path
			return
		}

		oldPath = ctx.url.Path
		if len(parts) < 2 {
			ctx.url.Path = "/"
		} else if path.IsAbs(parts[1]) {
			ctx.url.Path = parts[1]
		} else {
			ctx.url.Path = path.Join(ctx.url.Path, parts[1])
		}
		return
	case "get", "delete":
		method = strings.ToUpper(parts[0])
	case "post", "put", "patch":
		method = strings.ToUpper(parts[0])
		parser := flag.NewFlagSet("request body", flag.ContinueOnError)
		v := parser.String("v", "", "set value directly")
		f := parser.String("f", "", "set value from file")
		err := parser.Parse(parts[1:])
		if err != nil {
			fmt.Println("err: " + err.Error())
			return
		}

		if *v != "" {
			body = strings.NewReader(*v)
		}
		if *f != "" {
			var file *os.File
			file, err = os.Open(*f)
			if err != nil {
				fmt.Println("err: " + err.Error())
				return
			}
			defer func() {
				_ = file.Close()
			}()
			body = file
		}
	}
	if method != "" {
		req, err := http.NewRequest(method, ctx.url.String(), body)
		if err != nil {
			fmt.Println("err: " + err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		var resp *http.Response
		resp, err = ctx.client.Do(req)
		if err != nil {
			fmt.Println("err: " + err.Error())
			return
		}
		_, _ = io.Copy(os.Stdout, resp.Body)
		_ = resp.Body.Close()
		return
	}
	fmt.Println("Sorry, I don't understand.")
}

func completer(in prompt.Document) (match []prompt.Suggest) {
	w := in.GetWordBeforeCursor()
	if w != "" {
		match = prompt.FilterHasPrefix(command, w, true)
		if len(match) != 0 {
			return match
		}
	}

	var lastKey string
	if w == "" {
		lastKey = strings.TrimSpace(in.GetWordBeforeCursorWithSpace())
	} else {
		parts := strings.Fields(in.TextBeforeCursor())
		if len(parts) > 1 {
			lastKey = parts[len(parts)-2]
		}
	}

	switch lastKey {
	case "cd":
		var (
			request string
			join    = strings.Contains(w, "/")
			dir     = path.Dir(w)
			resp    *http.Response
			err     error
			res     interface{}
		)
		if join {
			var u = *ctx.url
			if path.IsAbs(dir) {
				u.Path = dir
				request = u.String()
			} else {
				u.Path = path.Join(u.Path, dir)
				request = u.String()
			}
		} else {
			request = ctx.url.String()
		}
		if request == pathCache {
			match = pathKeys
			goto pathFilter
		}
		resp, err = ctx.client.Get(request)
		if err != nil {
			fmt.Println("err: " + err.Error())
			return []prompt.Suggest{}
		}

		_ = json.NewDecoder(resp.Body).Decode(&res)
		_ = resp.Body.Close()

		switch v := res.(type) {
		case []interface{}:
			match = make([]prompt.Suggest, len(v), len(v)+2)
			for i := 0; i < len(match); i++ {
				text := strconv.Itoa(i)
				if join {
					text = path.Join(dir, text)
				}
				match[i] = prompt.Suggest{
					Text:        text,
					Description: "Array index",
				}
			}
		case map[string]interface{}:
			match = make([]prompt.Suggest, len(v), len(v)+2)
			i := 0
			for k := range v {
				if join {
					k = path.Join(dir, k)
				}
				match[i] = prompt.Suggest{
					Text:        k,
					Description: "Map key",
				}
				i++
			}
		}
		pathCache = request
		pathKeys = match
	pathFilter:
		match = prompt.FilterHasPrefix(match, w, true)
		match = append(match, prompt.Suggest{Text: "/config", Description: "Config root"})
	case "-f":
		var (
			cwd  = "."
			join = strings.Contains(w, string(os.PathSeparator))
			dir  = filepath.Dir(w)
			dirs []os.DirEntry
			err  error
		)
		if join {
			if filepath.IsAbs(dir) {
				cwd = dir
			} else {
				cwd = filepath.Join(cwd, dir)
			}
		}
		if cwd == fileCache {
			match = fileKeys
			goto fileFilter
		}
		dirs, err = os.ReadDir(cwd)
		if err != nil {
			fmt.Println("err: " + err.Error())
			return []prompt.Suggest{}
		}
		match = make([]prompt.Suggest, len(dirs))
		for i, info := range dirs {
			name := info.Name()
			if join {
				name = filepath.Join(dir, name)
			}
			match[i] = prompt.Suggest{
				Text:        name,
				Description: "file name",
			}
		}
		fileCache = cwd
		fileKeys = match
	fileFilter:
		match = prompt.FilterHasPrefix(match, w, true)
	}

	return match
}

func main() {
	var baseURL = "http://localhost:2019/"
	if len(os.Args) == 2 {
		baseURL = os.Args[1]
		if strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		log.Fatal(err)
	}
	ctx = &RequestContext{
		url:    u,
		client: &http.Client{},
	}

	p := prompt.New(
		executor,
		completer,
		prompt.OptionPrefix(u.String()+"> "),
		prompt.OptionLivePrefix(livePrefix),
		prompt.OptionTitle("http-prompt"),
	)
	p.Run()
}
