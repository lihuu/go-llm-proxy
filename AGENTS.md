# Agent notes

## Deploying to the Mac mini

**Do not stop the proxy before building/copying the binary.**

The proxy runs on the Mac mini under launchd (`com.llm-proxy`). Coding agents
that deploy this project also *use* the proxy for their own LLM calls. If you
`launchctl bootout` / `kill` the proxy first, then build + `scp`, the proxy is
down for minutes — and you (the agent doing the deploy) will time out on your
own LLM calls mid-deploy.

### Correct flow — use the deploy script

From the repo root:

```bash
scripts/deploy-macmini.sh
```

It builds locally (`GOOS=darwin GOARCH=arm64`), `scp`s the new binary as
`go-llm-proxy.new`, then over SSH atomically swaps it in and runs
`launchctl bootout` + `launchctl bootstrap`. The proxy is only down for the
~3-second bootout→bootstrap gap.

### If you must do it manually

1. Build locally: `GOOS=darwin GOARCH=arm64 go build -o go-llm-proxy .`
2. Copy: `scp go-llm-proxy macmini:~/deploy/llm-proxy/go-llm-proxy.new`
3. Swap + restart on the mini:

   ```bash
   ssh macmini 'cd ~/deploy/llm-proxy && mv -f go-llm-proxy.new go-llm-proxy && chmod +x go-llm-proxy && launchctl bootout gui/$(id -u)/com.llm-proxy 2>/dev/null; sleep 1; launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.llm-proxy.plist'
   ```

`bootout` without a following `bootstrap` leaves the service **unloaded** —
`KeepAlive` will not restart it because the service is no longer registered
with launchd. Always pair them.

See [docs/deploy-macmini.md](docs/deploy-macmini.md) for details.