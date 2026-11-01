#!/usr/bin/env bash
# Registers the companion as a Chrome native messaging host for the current
# user on Linux or macOS. Run it after building companion/bin/geomind-companion.
set -euo pipefail

HOST_NAME="com.geomind.companion"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY="${1:-$ROOT/companion/bin/geomind-companion}"
MANIFEST="$ROOT/extension/manifest.json"

if [[ ! -f "$MANIFEST" ]]; then
  echo "extension manifest not found: $MANIFEST" >&2
  exit 1
fi
if [[ ! -x "$BINARY" ]]; then
  echo "companion binary not found or not executable: $BINARY" >&2
  echo "build it with: (cd companion && go build -o bin/geomind-companion ./cmd/geomind-companion)" >&2
  exit 1
fi
if ! command -v node >/dev/null 2>&1; then
  echo "node is required to derive the extension id from manifest.json" >&2
  exit 1
fi

BINARY="$(cd "$(dirname "$BINARY")" && pwd)/$(basename "$BINARY")"

EXTENSION_ID="$(node -e '
const fs = require("node:fs");
const crypto = require("node:crypto");
const manifest = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (typeof manifest.key !== "string") throw new Error("manifest key is missing");
const hash = crypto.createHash("sha256").update(Buffer.from(manifest.key, "base64")).digest().subarray(0, 16);
let id = "";
for (const byte of hash) id += "abcdefghijklmnop"[byte >> 4] + "abcdefghijklmnop"[byte & 15];
process.stdout.write(id);
' "$MANIFEST")"

case "$(uname -s)" in
  Linux)
    HOST_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/google-chrome/NativeMessagingHosts"
    ;;
  Darwin)
    HOST_DIR="$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts"
    ;;
  *)
    echo "unsupported OS: $(uname -s); use scripts/install-native-host.ps1 on Windows" >&2
    exit 1
    ;;
esac

TARGET="$HOST_DIR/$HOST_NAME.json"
mkdir -p "$HOST_DIR"
node -e '
const fs = require("node:fs");
const [target, name, binary, id] = process.argv.slice(1);
const manifest = {
  name,
  description: "GeoMind companion",
  path: binary,
  type: "stdio",
  allowed_origins: ["chrome-extension://" + id + "/"],
};
fs.writeFileSync(target, JSON.stringify(manifest, null, 2) + "\n", { mode: 0o644 });
' "$TARGET" "$HOST_NAME" "$BINARY" "$EXTENSION_ID"

printf 'Installed %s\n  manifest: %s\n  binary:   %s\n  extension: %s\nReload the extension in chrome://extensions and restart Chrome.\n' \
  "$HOST_NAME" "$TARGET" "$BINARY" "$EXTENSION_ID"
