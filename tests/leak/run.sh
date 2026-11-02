#!/bin/sh
# Leak suite entry point. Each stage skips only when its prerequisites are
# missing; a failing assertion fails the run.
#
# CI does not call this script: the workflow runs the same stages as separate
# steps with LEAK_REQUIRE_NETNS=1 so a skipped netns capture fails the build.
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
