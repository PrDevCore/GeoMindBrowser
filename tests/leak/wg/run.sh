#!/bin/sh
# Step 4 tunnel verification inside a Linux network namespace.
#
# The tests run with only loopback up, so a passing run proves the tunnel and
# DoH path need no host connectivity. tcpdump records every packet: all
# host-visible traffic must be WireGuard UDP between 127.0.0.1 ports. DNS on
# port 53, TCP, ICMP, or any other address is a failure.
#
# Requires Linux and root. Prints "skip" and exits 0 elsewhere, so the Windows
# development machine is unaffected.
set -eu

if [ "$(uname -s)" != "Linux" ]; then
	echo "skip: the network namespace suite is Linux only"
	exit 0
fi
if [ "$(id -u)" -ne 0 ]; then
	echo "skip: root is required for network namespaces and capture"
	exit 0
fi
for tool in unshare ip tcpdump go; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "skip: $tool not found"
		exit 0
	fi
done

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$here/../../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cd "$root/companion"
go test -c -o "$tmp/tunnel.test" ./internal/tunnel

cat >"$tmp/inside.sh" <<'EOF'
#!/bin/sh
set -eu

tmp=$1
ip link set lo up

tcpdump -i lo -n -l 'ip' >"$tmp/capture.txt" 2>"$tmp/tcpdump.err" &
tcpdump_pid=$!
sleep 1

set +e
"$tmp/tunnel.test" \
	-test.run 'TestTwoPeersCarryTrafficAndDoH|TestKillPeerStopsTrafficAndGoesUnhealthy|TestHandshakeFailureStaysUnhealthy' \
	-test.v -test.timeout 120s >"$tmp/test.log" 2>&1
status=$?
set -e

sleep 1
kill "$tcpdump_pid" 2>/dev/null || true
wait "$tcpdump_pid" 2>/dev/null || true

cat "$tmp/test.log"
if [ "$status" -ne 0 ]; then
	echo "FAIL: tunnel tests failed"
	exit 1
fi

if ! grep -q 'UDP' "$tmp/capture.txt"; then
	echo "FAIL: no WireGuard UDP was captured"
	exit 1
fi
if grep -q '\.53[: ]' "$tmp/capture.txt"; then
	echo "FAIL: DNS traffic appeared on the wire"
	exit 1
fi
if grep -q 'Flags \[' "$tmp/capture.txt"; then
	echo "FAIL: TCP appeared on the wire"
	exit 1
fi
if grep -q 'ICMP' "$tmp/capture.txt"; then
	echo "FAIL: ICMP appeared on the wire"
	exit 1
fi
if ! awk '/ IP / {
		if ($0 !~ / IP 127\.0\.0\.1\.[0-9]+ > 127\.0\.0\.1\.[0-9]+:/ || $0 !~ /: UDP,/) {
			bad = 1
		}
	}
	END { exit bad }' "$tmp/capture.txt"; then
	echo "FAIL: captured a packet that was not loopback WireGuard UDP"
	cat "$tmp/capture.txt"
	exit 1
fi

echo "PASS: only loopback WireGuard UDP crossed the wire"
EOF

unshare -n sh "$tmp/inside.sh" "$tmp"
