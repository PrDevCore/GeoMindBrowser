#!/bin/sh
# Step 3 fail-closed checks for the loopback proxy. These use a fake Tunnel and
# need no privileges: they prove the proxy refuses and tears down connections
# when the tunnel is unhealthy, and that the package has no direct dial path.
#
# The privileged Linux netns capture suites (DNS, WebRTC, kill switch against a
# real WireGuard peer) live in tests/leak/wg and tests/leak/browser from Step 4
# and Step 5 onward.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$here/../../.." && pwd)

cd "$root/companion"

go test -count=1 -v ./internal/proxy -run 'TestRefuses|TestForwards|TestTargetAddresses|TestRejects|TestKillMidTransfer|TestCloseFailsInFlight|TestNewServer|TestListenBinds|TestPackageHasNoDirectDialPath'
go test -count=1 -v ./internal/tunnel -run 'TestSupervisor'
