# Deploying to Mac mini (minimal downtime)

## The pitfall

When a coding agent (Qwen Code, etc.) deploys this proxy, the naive flow is:

1. `launchctl bootout` / `kill` the running proxy.
2. Build the binary locally.
3. `scp` it to the Mac mini.
4. `launchctl bootstrap` to start it again.

Step 1 kills the proxy, and steps 2–3 take **minutes** (build + copy). During
that window the proxy is unreachable. The agent that is *doing the deploy* also
relies on the proxy for its own LLM calls, so it times out (~27s) mid-deploy.

## The fix

Keep the proxy up while building and copying. Only take it down for the final
`bootout → bootstrap` swap, which takes ~3 seconds.

Use `scripts/deploy-macmini.sh`:

```bash
# From the repo root:
scripts/deploy-macmini.sh
```

What it does:

1. Builds `GOOS=darwin GOARCH=arm64` locally (proxy on the mini stays up).
2. `scp`s the binary to `macmini:~/deploy/llm-proxy/go-llm-proxy.new`
   (proxy still up — it's just a file copy).
3. Over SSH: `mv go-llm-proxy.new → go-llm-proxy`, then
   `launchctl bootout` + `launchctl bootstrap`.

The `bootout` → `bootstrap` gap is ~3s. `KeepAlive: true` in the plist does
*not* rescue you if you `bootout` without `bootstrap`, because `bootout`
fully unloads the service.

## For agents

If you are a coding agent deploying this project:

- **Do not** `launchctl bootout` / `kill` the proxy before building/copying.
- **Do** run `scripts/deploy-macmini.sh` from the repo root. It does
  build → copy → restart, in that order.
- If you must do it manually, the sequence on the mini is:

  ```bash
  launchctl bootout gui/$(id -u)/com.llm-proxy   # ~1s
  launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.llm-proxy.plist
  ```

  and `~/deploy/llm-proxy/restart.sh` already wraps this.

## Restart-only

If the binary is already in place and you only need to restart:

```bash
ssh macmini ~/deploy/llm-proxy/restart.sh
```