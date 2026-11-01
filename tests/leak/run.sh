#!/bin/sh
# Leak suite entry point. Each stage skips only when its prerequisites are
# missing; a failing assertion fails the run.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"

echo "== companion fail-closed checks =="
sh tests/leak/companion/run.sh

echo "== WireGuard netns capture =="
sh tests/leak/wg/run.sh

echo "== browser PAC and fail-closed checks =="
if [ ! -d node_modules/@playwright ]; then
  echo "SKIP: run npm install && npx playwright install chromium first"
else
  npm run test:browser
fi
