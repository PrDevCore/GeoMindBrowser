// Command geomind-companion is the native messaging host. It serves the typed
// IPC protocol on stdin/stdout and owns the data plane: a loopback SOCKS5
// listener backed by the WireGuard tunnel (or a closed tunnel when none is
// configured). It has no TCP control port.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"geomind/companion/internal/exitinfo"
	"geomind/companion/internal/ipc"
	"geomind/companion/internal/policy"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "geomind-companion:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("geomind-companion", flag.ContinueOnError)
	fs.SetOutput(stderr)
	policyPath := fs.String("policy", os.Getenv("GEOMIND_POLICY"), "path to a policy JSON file")
	knownExits := fs.String("known-exits", os.Getenv("GEOMIND_KNOWN_EXITS"), "comma-separated exit identifiers")
	exitID := fs.String("exit-id", os.Getenv("GEOMIND_EXIT_ID"), "exit identifier reported in tunnel status")
	proxyAddr := fs.String("proxy-addr", "127.0.0.1:0", "loopback address for the SOCKS5 proxy; port 0 picks an ephemeral port")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	info, err := exitinfo.FromEnv(os.Getenv)
	if err != nil {
		return err
	}

	var store *policy.Store
	var policyStart *fileStamp
	if *policyPath != "" {
		store = policy.New(splitList(*knownExits))
		// Capture the stamp before loading so a write during startup is not
		// mistaken for the already-loaded state.
		if stamp, ok := policyStamp(*policyPath); ok {
			policyStart = &stamp
		}
		if _, err := store.Load(*policyPath); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	rt, err := startRuntime(ctx, runtimeOptions{
		ProxyAddr: *proxyAddr,
		ExitID:    *exitID,
		Region:    info.Region,
		Locale:    info.Locale,
		Getenv:    os.Getenv,
	})
	if err != nil {
		return err
	}
	defer rt.Close()
	go rt.Serve(ctx)

	sender := ipc.NewSender(stdout)
	if store != nil {
		go watchPolicy(ctx, *policyPath, store, sender, policyWatchInterval, policyStart)
	}

	deps := ipc.Deps{
		Policy: func() (policy.Doc, bool) {
			if store == nil {
				return policy.Doc{}, false
			}
			return store.Snapshot()
		},
		PolicyAndPAC: func() (policy.Doc, string, bool, error) {
			if store == nil {
				return policy.Doc{}, "", false, nil
			}
			return store.SnapshotWithPAC(rt.ProxyAddr())
		},
		Tunnel: rt.Status,
	}

	return ipc.ServeWithSender(ctx, stdin, sender, ipc.Handlers(deps))
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
