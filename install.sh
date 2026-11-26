#!/bin/sh
# Install loggy — the latest release from GitHub.
#
# Usage:
#   curl -sSfL https://raw.githubusercontent.com/shreyas-shriyan/loggy/main/install.sh | sh
#
# Override the install directory:
#   INSTALL_DIR=~/.local/bin curl -sSfL ... | sh
set -e

REPO="shreyas-shriyan/loggy"
INSTALL_DIR="${INSTALL_DIR:-}"

# ── OS / arch detection ────────────────────────────────────────────────────────
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$arch" in
  x86_64)        arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "Unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

case "$os" in
  linux|darwin) ;;
  *)
    echo "Unsupported OS: $os" >&2
    echo "For Windows, download the .zip from: https://github.com/${REPO}/releases/latest" >&2
    exit 1
    ;;
esac

# ── HTTP fetch helper ─────────────────────────────────────────────────────────
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -sSfL "$1"; }
  fetch_to() { curl -sSfL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
  fetch_to() { wget -qO "$2" "$1"; }
else
  echo "curl or wget is required" >&2
  exit 1
fi

# ── Resolve latest version ─────────────────────────────────────────────────────
latest="$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
          | grep '"tag_name"' \
          | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/')"

if [ -z "$latest" ]; then
  echo "Could not determine the latest release version." >&2
  exit 1
fi

echo "Installing loggy v${latest} (${os}/${arch})..."

# ── Build URLs ────────────────────────────────────────────────────────────────
base="loggy_${latest}_${os}_${arch}"
archive_url="https://github.com/${REPO}/releases/download/v${latest}/${base}.tar.gz"
checksum_url="https://github.com/${REPO}/releases/download/v${latest}/checksums.txt"

# ── Download to temp dir ──────────────────────────────────────────────────────
tmp="$(mktemp -d)"
# shellcheck disable=SC2064
trap 'rm -rf "$tmp"' EXIT

fetch_to "$archive_url"  "${tmp}/${base}.tar.gz"
fetch_to "$checksum_url" "${tmp}/checksums.txt"

# ── Verify checksum ───────────────────────────────────────────────────────────
cd "$tmp"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum --ignore-missing -c checksums.txt
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 --ignore-missing -c checksums.txt
else
  echo "Warning: sha256sum/shasum not found — skipping checksum verification." >&2
fi

# ── Extract binary ────────────────────────────────────────────────────────────
tar -xzf "${base}.tar.gz" loggy

# ── Choose install directory ──────────────────────────────────────────────────
if [ -z "$INSTALL_DIR" ]; then
  if [ -w /usr/local/bin ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "$INSTALL_DIR"
  fi
fi

mv loggy "${INSTALL_DIR}/loggy"
chmod +x "${INSTALL_DIR}/loggy"

echo ""
echo "loggy v${latest} installed to ${INSTALL_DIR}/loggy"

# ── PATH reminder ─────────────────────────────────────────────────────────────
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "NOTE: ${INSTALL_DIR} is not in your PATH."
    echo "Add the following line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac
