#!/bin/sh
# Step 4 tunnel verification inside a Linux network namespace.
#
# The tests run with only loopback up, so a passing run proves the tunnel and
# DoH path need no host connectivity. tcpdump records every packet, and the rule
# is that nothing may involve a non-loopback address: DNS on port 53, any TCP, and
# any packet whose source or destination is not 127.0.0.1 all fail the suite. The
# only protocols allowed on the wire are WireGuard UDP between 127.0.0.1 endpoints
# and the kernel's own loopback ICMP destination-unreachable replies, which cannot
# leave a namespace that has one interface and no route.
#
# Requires Linux and root. Prints "skip" and exits 0 elsewhere, so the Windows
# development machine is unaffected. CI sets LEAK_REQUIRE_NETNS=1, which turns
# every skip into a failure: a suite that never ran cannot prove the tunnel
# invariant, so a silent skip would be a false green.
#
# CI also sets LEAK_TUNNEL_TEST to a binary built by the unprivileged runner user.
# Compiling as root would miss the restored Go build cache and resolve a second
# toolchain, so the privileged step only ever runs a prebuilt binary.
set -eu

blocked() {
	echo "$1"
	if [ "${LEAK_REQUIRE_NETNS:-}" = "1" ]; then
		echo "FAIL: LEAK_REQUIRE_NETNS=1 forbids skipping the network namespace suite"
		exit 1
	fi
	exit 0
}

if [ "$(uname -s)" != "Linux" ]; then
	blocked "skip: the network namespace suite is Linux only"
fi
if [ "$(id -u)" -ne 0 ]; then
	blocked "skip: root is required for network namespaces and capture"
fi
for tool in unshare ip tcpdump; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		blocked "skip: $tool not found (PATH=$PATH)"
	fi
done

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$here/../../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# CI builds the test binary as the unprivileged runner user and passes it in, so
# the root step never touches a toolchain or a cold module cache. Without that
# variable this script compiles the binary itself, which is what a local
# one-command run needs.
if [ -n "${LEAK_TUNNEL_TEST:-}" ]; then
	if [ ! -x "$LEAK_TUNNEL_TEST" ]; then
		echo "FAIL: LEAK_TUNNEL_TEST is not an executable file: $LEAK_TUNNEL_TEST"
		exit 1
	fi
	test_bin=$LEAK_TUNNEL_TEST
else
	if ! command -v go >/dev/null 2>&1; then
		blocked "skip: go not found (PATH=$PATH)"
	fi
	cd "$root/companion"
	go test -c -o "$tmp/tunnel.test" ./internal/tunnel
	test_bin="$tmp/tunnel.test"
fi

cat >"$tmp/inside.sh" <<'EOF'
#!/bin/sh
set -eu

tmp=$1
tunnel_test=$2
ip link set lo up

tcpdump -i lo -n -l 'ip' >"$tmp/capture.txt" 2>"$tmp/tcpdump.err" &
tcpdump_pid=$!
sleep 1

set +e
"$tunnel_test" \
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
	cat "$tmp/tcpdump.err"
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
# Every ICMP line is echoed for the record before it is judged: this suite has
# never been observed in a real namespace before CI, so the packet text is the
# evidence that the rule below is the right one.
grep 'ICMP' "$tmp/capture.txt" | sed 's/^/icmp: /' || true
# A UDP send to a peer the tests deliberately stopped makes the kernel answer with
# ICMP destination-unreachable between 127.0.0.1 addresses. That packet cannot
# leave the namespace, which has one interface and no route, so it is allowed.
# ICMP involving any other address is a leak and fails.
if ! awk '/ IP / {
		if ($0 !~ / IP 127\.0\.0\.1(\.[0-9]+)? > 127\.0\.0\.1(\.[0-9]+)?: (UDP,|ICMP)/) {
			bad = 1
		}
	}
	END { exit bad }' "$tmp/capture.txt"; then
	echo "FAIL: captured a packet that was not loopback-only"
	cat "$tmp/capture.txt"
	exit 1
fi

echo "PASS: every captured packet stayed on loopback (WireGuard UDP, kernel ICMP)"
EOF

unshare -n sh "$tmp/inside.sh" "$tmp" "$test_bin"
