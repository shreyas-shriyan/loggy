# loggy — Reference

## Contents

- [Dashboard](#dashboard)
- [Sign-in](#sign-in)
- [Configuration](#configuration)
- [AWS Permissions](#aws-permissions)
- [Development](#development)

---

## Dashboard

`loggy`, `loggy dev|prod`, and `loggy menu` open a full-screen TUI: saved targets on the
left, the live log pane on the right, and key hints at the bottom.

Log lines are coloured by level (`ERR` `WRN` `INF` `DBG`), timestamps and pod prefixes are
dimmed, and JSON is pretty-printed. Colours adapt to light and dark terminals.

### Navigation

| Key | Action |
|-----|--------|
| `↑` / `↓`  (`k` / `j`) | Move through the target list |
| `enter` | Stream live logs (last 15 min, health checks hidden) |
| `a` | Stream live logs including health checks |
| `c` | Custom stream — time range, keyword/regex filter, single pod (EKS) |
| `e` | Switch between dev and prod |
| `tab` | Toggle focus between target list and log pane |
| `q` | Quit |

### Log pane

| Key | Action |
|-----|--------|
| `s` | Save logs to `logs/<env>-<service>-<timestamp>.log` |
| `x` / `ctrl+c` | Stop the current stream |
| `h` | Show / hide health-check lines (no restart) |
| `J` | Toggle pretty ↔ compact JSON |
| `r` | Toggle raw mode (see note below) |
| `w` | Toggle line wrapping |
| `z` | Zoom: hide the sidebar for a full-width log pane (`esc` / `tab` to restore) |
| `↑↓` `pgup/pgdn` mouse wheel | Scroll; `g` / `G` jump to top / follow |
| mouse drag | Select text in the log pane |
| `y` | Copy selection (or all visible lines if nothing selected) |
| `esc` | Clear selection |

### Server management

| Key | Action |
|-----|--------|
| `n` | Add a target |
| `d` | Remove targets |
| `p` | Configure AWS profile and region |
| `S` | Save an unsaved target (from `loggy menu`) |

> **Raw mode (`r`):** trims the full pod name and repeated logger prefixes (NestJS app name,
> PID, date), summarises HTTP access logs as `GET /path 200 12ms`, and collapses trailing
> JSON as `{…}`. Warnings and errors always show the full line.

> **Clipboard:** `clip.exe` on Windows/WSL, `pbcopy` on macOS, `wl-copy`/`xclip`/`xsel`
> on Linux, falling back to OSC 52. Wrapped lines and pretty JSON preserve their line breaks
> in the copy; use `J` for compact JSON first, or `y` for the original unwrapped text.

The log pane keeps the latest **5,000 lines**. Forms (add target, custom range, SSO sign-in)
temporarily take over the screen and return to the dashboard when done.

---

## Sign-in

**Credential check:** loggy calls `sts:GetCallerIdentity` on startup. If a profile's
credentials are valid, no further sign-in is needed.

**Browser SSO (PKCE):** Profiles with an `[sso-session]` block sign in through the browser
automatically — loggy starts a short-lived server on `127.0.0.1`, opens the authorization
URL, and writes the token to `~/.aws/sso/cache`, exactly like `aws sso login`.

**Device-code SSO:** Older SSO profiles, headless machines (SSH), or setting
`LOGGY_SSO_DEVICE_CODE=1` fall back to device-code flow — loggy prints a URL to open on
any device and waits for confirmation.

**Other credential types** (IAM keys, assumed roles, etc.) must be refreshed outside loggy.

**Creating a profile:** choose **Add a new AWS profile…** in the profile picker (`p` in the
dashboard, or at the first `loggy setup`). For SSO, loggy runs through the same flow as
`aws configure sso` and appends the profile to `~/.aws/config`. For access-key auth, the
key is written to `~/.aws/credentials`.

---

## Configuration

loggy reads its settings from a YAML file (location controlled by `LOGGY_CONFIG`):

| Variable | Default | Description |
|----------|---------|-------------|
| `LOGGY_CONFIG` | `~/.config/loggy/config.yaml`<br>(macOS: `~/Library/Application Support/loggy/config.yaml`) | Config file path (written with `0600`) |
| `LOGGY_LOG_DIR` | `./logs` | Directory for saved log files |
| `LOGGY_SSO_DEVICE_CODE` | _(unset)_ | Set to `1` to always use device-code SSO (e.g. over SSH) |

**Example `config.yaml`:**

```yaml
environments:
  dev:
    profile: example-dev
    region: us-east-1
  prod:
    profile: example-prod
    region: us-east-1

targets:
  - name: orders-api
    env: dev
    type: eks
    cluster: demo-cluster
    namespace: shop
    service: orders-api

  - name: billing-worker
    env: prod
    type: ecs
    cluster: arn:aws:ecs:us-east-1:123456789012:cluster/demo
    service: arn:aws:ecs:us-east-1:123456789012:service/demo/billing-worker
    log_group: /ecs/billing-worker

# Optional: regexes for health-check lines hidden by default.
health_patterns:
  - 'GET /health(z|check)? HTTP/1\.[01]'
  - 'ELB-HealthChecker'
```

A target's `profile` / `region` keys override its environment's defaults.  
When `health_patterns` is omitted, loggy hides `GET /health`, `/healthz`, `/healthcheck`,
`/livez`, `/readyz`, `ELB-HealthChecker`, and `kube-probe` requests. Setting it replaces
that list entirely.

---

## AWS Permissions

loggy makes the same API calls as the AWS CLI — no write access is needed:

| Service | Calls |
|---------|-------|
| STS | `GetCallerIdentity` |
| EKS | `ListClusters`, `DescribeCluster` |
| Kubernetes | Read access to namespaces, services, pods, and logs |
| ECS | `List*`, `Describe*` |
| CloudWatch Logs | `FilterLogEvents` (polled every 2 s — **not** the paid Live Tail) |

---

## Development

```bash
make test vet   # run tests and go vet
make build      # build bin/loggy for the current platform
make cross      # cross-compile for all platforms → dist/
make snapshot   # local GoReleaser build (no publish, no tag required)
```

**Package layout:**

| Package | Purpose |
|---------|---------|
| `cmd/loggy` | Entrypoint |
| `internal/cli` | Commands and menus |
| `internal/config` | YAML config |
| `internal/awsauth` | Profiles, SSO |
| `internal/eks` / `internal/ecs` | AWS service clients |
| `internal/cwlogs` | CloudWatch Logs tail |
| `internal/stream` | Filters, file saves, Ctrl+C |
| `internal/ui` | Dashboard and log formatting |
| `internal/tui` | Form prompts |

Releases are built with [GoReleaser](https://goreleaser.com) and published automatically
when a `v*` tag is pushed (see `.github/workflows/release.yml`).
