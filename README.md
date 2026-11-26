# loggy

[![Latest Release](https://img.shields.io/github/v/tag/shreyas-shriyan/loggy?label=release&sort=semver)](https://github.com/shreyas-shriyan/loggy/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> Interactive terminal log viewer for **EKS** and **ECS** services on AWS —  
> a single Go binary for macOS, Linux/WSL and Windows.

Stream and save pod logs (EKS) or CloudWatch Logs (ECS) across `dev` and `prod` environments.
loggy talks directly to AWS and Kubernetes SDKs — no `aws` CLI, no `kubectl`, and it never
touches `~/.kube/config`.

| Target | Live stream | Save to file | Notes |
|--------|-------------|--------------|-------|
| EKS    | ✓           | ✓            | All pods behind a Service, or a single pod |
| ECS    | ✓           | ✓            | Log group read from the task definition |

---

## Install

### macOS / Linux — one-liner

```bash
curl -sSfL https://raw.githubusercontent.com/shreyas-shriyan/loggy/main/install.sh | sh
```

Downloads the latest release, verifies the SHA-256 checksum, and installs to `/usr/local/bin`
(falls back to `~/.local/bin`). Override with `INSTALL_DIR`:

```bash
INSTALL_DIR=~/.local/bin curl -sSfL https://raw.githubusercontent.com/shreyas-shriyan/loggy/main/install.sh | sh
```

### Go

```bash
go install github.com/shreyas-shriyan/loggy/cmd/loggy@latest
```

### Windows

Download the `.zip` for your architecture from the
[Releases page](https://github.com/shreyas-shriyan/loggy/releases/latest) and extract
`loggy.exe` somewhere on your `PATH`.

### From source

```bash
git clone https://github.com/shreyas-shriyan/loggy.git && cd loggy
make build   # → bin/loggy
make cross   # → dist/loggy-<os>-<arch>[.exe]
```

### Updating

```bash
loggy update   # pull the latest release from GitHub
```

---

## Quick start

```bash
loggy setup dev      # pick an AWS profile/region, then add EKS or ECS targets
loggy                # pick dev/prod and open the dashboard
loggy dev            # open the dev dashboard directly
loggy menu           # browse clusters/services without saving
loggy targets list   # print saved targets
loggy targets rm     # remove saved targets
```

---

## Docs

Full reference documentation is in [docs/reference.md](docs/reference.md), covering:

- Dashboard keybinds and log-pane controls
- AWS sign-in flows (SSO browser/PKCE, device code, profile creation)
- Configuration (env vars and config.yaml reference)
- AWS permissions and cost
- Development, build commands, and package layout

---

## License

[MIT](LICENSE)
