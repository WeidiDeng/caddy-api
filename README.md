# Caddy Api

## Compile
```
git clone https://github.com/WeidiDeng/caddy-api
cd caddy-api && go build
```

Then move caddy-api to ~/go/bin.

Because this module uses replace directive, currently there is no way to one line install.

## Usage
```
caddy-api (caddy admin address, default http://localhost:2019)
```

Then it's interactive. There are following commands in this mode.

### General Command
```
cd Change URL/path
pwd Show current URL/path
exit Exit caddy-api
```

### Request Command

These will do corresponding request at current URL/path. All except "delete" and "get" requires a "-v" or "-f" flag to input the new value. "-v" means new value is as a minified json argument, "-f" means new value is loaded from a file at the specified path (with autocompletion of cause).

```
delete
get 
patch
post
put
```
