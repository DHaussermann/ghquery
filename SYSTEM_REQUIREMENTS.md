# System Requirements

To run `ghquery` you need three things installed and configured on each user's machine:

## 1. Go (build dependency only)

Required only to compile the binary.

- Go 1.23 or later
- Install: https://go.dev/dl/ or `brew install go`

If you receive a pre-compiled binary from someone else, you can skip Go entirely.

## 2. Claude Code CLI

`ghquery` shells out to the local `claude` command for every PR risk analysis. Each "subagent" (alpha / beta / gamma) is a separate `claude -p` process running on your machine. Without `claude`, the tool can fetch PRs but every analysis returns `UNKNOWN`.

### Install

```bash
npm install -g @anthropic-ai/claude-code
```

(Requires Node.js 18 or later. `brew install node` if needed.)

### Authenticate

Run `claude` once interactively and log in:

```bash
claude
```

Follow the OAuth prompts. Alternatively, set `ANTHROPIC_API_KEY` in your environment.

### Verify

```bash
which claude        # should print a path
claude --version    # should print a version
echo "ping" | claude -p   # should produce a short response
```

If any of these fail, fix the install before running `ghquery`.

### Cost

Each PR analysis is one `claude` invocation. With Sonnet 4.6 (1M context) and an average PR of ~50 KB of diff:
- ~10,000–30,000 input tokens per PR
- ~2,000 output tokens per PR

Costs are billed to **your** Anthropic account (Pro/Max subscription or API credits). One scheduled daily run analyzing 5–10 PRs is well within a personal Pro plan.

## 3. GitHub Personal Access Token (recommended)

Without a token, `ghquery` is rate-limited to 60 GitHub API requests per hour, which runs out quickly. With a token, the limit is 5,000 / hour — comfortable headroom for a daily scheduled run across 3 repos.

### Generate

1. Go to https://github.com/settings/tokens/new
2. Note: `ghquery`
3. Expiration: your choice
4. Scopes: **none required** (public repos only)
5. Click **Generate token** and copy the value immediately

### Authorize for your org (SAML SSO)

If your organization uses SAML SSO:
1. Go to https://github.com/settings/tokens
2. Click **Configure SSO** next to your token
3. **Authorize** your org and complete the SAML redirect

### Configure

Paste the token into `config.yaml`:

```yaml
github_token: "ghp_..."
```

## 4. incoming webhook (optional)

Required only if you want the report posted to the channel. You can use `ghquery` for terminal/UI output without one.

### Create

1. In your chat tool: create an incoming webhook (e.g. Slack: Apps → Incoming Webhooks; or your chat tool's equivalent)
2. Pick a channel and a name
3. Copy the URL

### Configure

Paste into `config.yaml`:

```yaml
webhook_url: "https://your-chat-server.example.com/hooks/..."
```

The webhook URL must be reachable from the machine that runs `ghquery`. If you're using a local webhook server via ngrok, ngrok must be running when scheduled jobs fire.

## 5. Operating System

`ghquery` works on macOS, Windows, and Linux. The scheduling feature uses each OS's native scheduler:

- **macOS** — launchd (LaunchAgents)
- **Windows** — Task Scheduler (`schtasks`)
- **Linux** — crontab

No additional setup beyond having one of these OSes.

## Quick start checklist

```
[ ] Node.js 18+ installed
[ ] Claude Code CLI installed and authenticated   (test: claude --version)
[ ] GitHub token generated and SSO-authorized
[ ] cp config.example.yaml config.yaml            (start from the template)
[ ] Edit config.yaml: github_token, webhook_url, catalog.teams, catalog.repos
[ ] go build -o ghquery .
[ ] ./ghquery run  →  works
[ ] ./ghquery ui   →  browser opens, can preview report
```

## Config

`config.yaml` has four sections:

- **`catalog:`** — your roster: which repos, which teams, which author display names. The UI populates menus from this section. Never overwritten by saves.
- **`query:`** — the saved scheduled query (what gets queried daily). Updated by the UI's "Save as Scheduled Query" button.
- **`schedule:`** — when the daily run fires. Managed by the UI's Schedule modal.
- **Connection keys** — `github_token`, `webhook_url`.

See `config.example.yaml` for a fully-commented template you can copy and edit. Comments in your live `config.yaml` are stripped on each save (a viper limitation), so keep `config.example.yaml` as your reference.

## Known limitations

- The `claude` binary must be on `$PATH` when `ghquery` runs. macOS launchd uses a minimal `$PATH` and may not see `claude` even when it works in your terminal — `ghquery schedule install` handles this by capturing your current `$PATH` into the launchd plist.
- A single Anthropic auth is required per user. There is no shared backend mode (yet).
- Per-PR analysis time scales with diff size; very large PRs (1000+ lines) may hit the 90-second per-agent timeout and fall back to `UNKNOWN`.
