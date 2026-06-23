#!/bin/sh
# punchcard installer — downloads the prebuilt `punch` binary for your OS/arch.
#   curl -fsSL https://raw.githubusercontent.com/ifokeev/punchcard/main/install.sh | sh
# Override install dir with:  INSTALL_DIR=~/.local/bin sh install.sh
set -eu

REPO="ifokeev/punchcard"
BIN="punch"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "punchcard: unsupported arch '$ARCH'" >&2; exit 1 ;;
esac
case "$OS" in
  darwin|linux) ;;
  *) echo "punchcard: unsupported OS '$OS' (build from source instead)" >&2; exit 1 ;;
esac

ASSET="${BIN}-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
TMP="$(mktemp)"

echo "punchcard: downloading ${ASSET}…"
if ! curl -fsSL "$URL" -o "$TMP"; then
  echo "punchcard: download failed ($URL)" >&2
  rm -f "$TMP"; exit 1
fi
chmod +x "$TMP"

if mv "$TMP" "${INSTALL_DIR}/${BIN}" 2>/dev/null; then
  :
elif command -v sudo >/dev/null 2>&1; then
  echo "punchcard: writing to ${INSTALL_DIR} (needs sudo)…"
  sudo mv "$TMP" "${INSTALL_DIR}/${BIN}"
else
  echo "punchcard: cannot write to ${INSTALL_DIR}; set INSTALL_DIR to a writable path" >&2
  rm -f "$TMP"; exit 1
fi

echo "punchcard: installed ${BIN} → ${INSTALL_DIR}/${BIN}"
echo "run:  punch serve"
