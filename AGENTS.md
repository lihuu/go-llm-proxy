# Agent notes

## Project overview

go-llm-proxy is a Go HTTP proxy that routes LLM API requests to multiple
backends. It runs as a launchd service (`com.llm-proxy`) on a Mac mini.

**Critical constraint:** the coding agent that deploys this project *also uses
this proxy for its own LLM calls*. If you stop the proxy before the new binary
is built and copied, you cut off your own LLM access mid-deploy and time out.

## Deploying to the Mac mini

### Rule #1: never stop the proxy before building/copying

The proxy must stay up during the build and copy phase. The only time it goes
down is the final `bootout → bootstrap` swap (~7s). Never do this:

```
# WRONG — kills proxy, then spends minutes building/copying while down
launchctl bootout gui/$(id -u)/com.llm-proxy   # proxy dead now
go build ...                                    # proxy still dead
scp ...                                         # proxy still dead
launchctl bootstrap ...                         # finally back, 5 min later
```

### Rule #2: use the deploy script

From the repo root, a single command handles everything in the correct order:

```bash
scripts/deploy-macmini.sh
```

What it does (proxy stays up through steps 1–2):

1. **Build locally** — `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build` (proxy still up on the mini)
2. **Copy as `.new`** — `scp go-llm-proxy macmini:~/deploy/llm-proxy/go-llm-proxy.new` (proxy still up)
3. **Swap + restart** — over SSH: `mv .new → go-llm-proxy`, then `launchctl bootout` + `bootstrap` (proxy down ~7s only)

### Rule #3: bootout must always be followed by bootstrap

`launchctl bootout` fully **unloads** the service from launchd. `KeepAlive: true`
in the plist will **not** restart it, because the service is no longer registered.
Always pair bootout with a bootstrap. If you only bootout, the proxy stays dead
until someone manually bootstraps it.

### Manual deploy (if the script is unavailable)

```bash
# 1. Build locally (proxy up)
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-s -w" -o go-llm-proxy .

# 2. Copy (proxy up)
scp go-llm-proxy macmini:~/deploy/llm-proxy/go-llm-proxy.new

# 3. Swap + restart (proxy down ~7s)
ssh macmini 'cd ~/deploy/llm-proxy && \
  mv -f go-llm-proxy.new go-llm-proxy && chmod +x go-llm-proxy && \
  launchctl bootout gui/$(id -u)/com.llm-proxy 2>/dev/null; sleep 1; \
  launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.llm-proxy.plist'

# 4. Verify
ssh macmini 'pgrep -fl go-llm-proxy'
```

### Restart only (binary already in place)

```bash
ssh macmini ~/deploy/llm-proxy/restart.sh
```

### Environment details

- **Mac mini SSH**: `ssh macmini` (192.168.2.8:22486, user lihu)
- **Deploy dir**: `~/deploy/llm-proxy/`
- **Binary**: `~/deploy/llm-proxy/go-llm-proxy`
- **Config**: `~/deploy/llm-proxy/config.yaml`
- **Plist**: `~/Library/LaunchAgents/com.llm-proxy.plist`
- **Launchd label**: `com.llm-proxy`
- **Listen**: `:8081`
- **Logs**: `~/deploy/llm-proxy/stdout.log`, `~/deploy/llm-proxy/stderr.log`
- **Target arch**: darwin/arm64 (Apple Silicon)

## Development

### Build & test

```bash
go build ./...          # compile
go vet ./...            # static analysis
go test ./...           # run tests
```

### Web assets

HTML/CSS/JS pages are embedded at compile time via `//go:embed` in `web/`.
Extraction of admin pages is still pending. See `web/web.go` for the embed
declarations and `web/configpage/`, `web/usagepage/` for extracted assets.

See [docs/deploy-macmini.md](docs/deploy-macmini.md) for deployment details.