# loggy

Interactive terminal log viewer for **EKS** (Kubernetes pod logs) and **ECS** (CloudWatch Logs) services on AWS, across `dev` and `prod` environments. A single Go binary for macOS, Linux/WSL and Windows.

loggy talks to AWS and Kubernetes through their SDKs, so it does **not** need the `aws` CLI or `kubectl` installed, and it never modifies `~/.kube/config`.

| Target type | Live logs | Save to file | Notes |
| --- | --- | --- | --- |
| EKS | yes | yes | all pods behind a Service, or a single pod |
| ECS | yes | yes | log group read from the task definition |

## Install

```bash
make build          # -> bin/loggy
make cross          # -> dist/loggy-<os>-<arch>[.exe]
```

Or build releases with [GoReleaser](https://goreleaser.com): `goreleaser release --snapshot --clean`.

## Quick start

```bash
loggy setup dev     # pick an AWS profile/region, then add EKS or ECS services
loggy               # pick dev/prod and open the dashboard
loggy dev           # open the dev dashboard directly
loggy menu          # browse clusters/services without saving
loggy targets list  # print saved services
loggy targets rm    # remove saved services
```

### Dashboard

`loggy`, `loggy dev|prod` and `loggy menu` open a full-screen dashboard: saved servers on the left, the live log pane on the right, and key hints at the bottom. Log lines are coloured by level (`ERR`, `WRN`, `INF`, `DBG`), timestamps and pod/stream prefixes are dimmed, and JSON log lines are pretty-printed. Colours adapt to light and dark terminals.

| Key | Action |
| --- | --- |
| `↑`/`↓` (`k`/`j`) | Select a server |
| `enter` | Live logs (last 15m, health checks hidden) |
| `a` | Live logs including health checks |
| `c` | Custom live logs: time range, keyword/regex filter, single pod (EKS) |
| `s` | Save logs to `logs/<env>-<service>-<timestamp>.log` |
| `x` / `ctrl+c` | Stop the current stream |
| `h` | Show/hide health-check lines (no restart) |
| `J` | Toggle pretty / compact JSON |
| `r` | Toggle raw lines. By default the full pod name and repeated logger prefixes (for example NestJS's app name, PID and date) are trimmed; warnings and errors always show the full line |
| mouse drag | Select any part of the log pane; releasing copies it to the clipboard as plain text |
| `y` | Copy the selection, or every visible log line if nothing is selected |
| `esc` | Clear the selection |
| `tab` | Switch focus between servers and logs (scroll with `↑↓`, `pgup`/`pgdn`, mouse wheel; `g`/`G` top/follow) |
| `n` / `d` / `p` | Add server / remove servers / configure AWS profile and region |
| `e` | Switch dev / prod |
| `S` | Save an unsaved server (from `loggy menu`) |
| `q` | Quit |

Forms (adding servers, custom ranges, SSO sign-in) briefly leave the dashboard and return to it afterwards. The log pane keeps the latest 5,000 lines.

Copying uses `clip.exe` on Windows and WSL, `pbcopy` on macOS, and `wl-copy`/`xclip`/`xsel` on Linux desktops, falling back to the terminal's OSC 52 clipboard. Selections copy what is on screen, so wrapped lines and pretty-printed JSON keep their line breaks; use `J` for compact JSON or `y` for the original unwrapped lines.

## Sign-in

loggy checks the profile's credentials with STS. If an IAM Identity Center (SSO) session has expired, it offers to sign in: it opens the browser and writes the token to `~/.aws/sso/cache`, exactly like `aws sso login`. Profiles that use other credential types must be refreshed outside loggy.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `LOGGY_CONFIG` | `~/.config/loggy/config.yaml` (macOS: `~/Library/Application Support/loggy/config.yaml`) | Settings and saved targets (written with `0600`) |
| `LOGGY_LOG_DIR` | `./logs` | Where saved log files go |

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

A target's `profile` / `region` override its environment's defaults. When `health_patterns` is omitted, loggy hides `GET /health`, `/healthz`, `/healthcheck`, `/livez`, `/readyz`, `ELB-HealthChecker` and `kube-probe` requests; setting it replaces that list.

## AWS permissions and cost

loggy uses `sts:GetCallerIdentity`, `eks:ListClusters`, `eks:DescribeCluster`, Kubernetes read access to namespaces/services/pods/logs, `ecs:List*`/`ecs:Describe*` and `logs:FilterLogEvents`. These are the same calls the AWS CLI makes; following CloudWatch logs polls `FilterLogEvents` every 2 seconds (it does not use the paid Live Tail feature).

## Development

```bash
make test vet
```

Layout: `cmd/loggy` (entrypoint), `internal/cli` (commands and menus), `internal/config` (YAML config), `internal/awsauth` (profiles, SSO), `internal/eks`, `internal/ecs`, `internal/cwlogs`, `internal/stream` (filters, files, Ctrl+C), `internal/ui` (dashboard and log formatting), `internal/tui` (form prompts).
