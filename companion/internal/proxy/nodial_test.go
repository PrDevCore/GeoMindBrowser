package proxy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageHasNoDirectDialPath enforces the fail-closed invariant: the proxy
// must reach the network only through Tunnel. A direct dial or local resolver
// call in this package would let routed traffic escape the tunnel.
func TestPackageHasNoDirectDialPath(t *testing.T) {
	banned := map[string]bool{
		"Dial":            true,
		"DialTimeout":     true,
		"DialIP":          true,
		"DialTCP":         true,
		"DialUDP":         true,
		"DialUnix":        true,
		"LookupAddr":      true,
		"LookupCNAME":     true,
		"LookupHost":      true,
		"LookupIP":        true,
		"LookupSRV":       true,
		"LookupTXT":       true,
		"LookupMX":        true,
		"LookupNS":        true,
		"ResolveIPAddr":   true,
		"ResolveTCPAddr":  true,
		"ResolveUDPAddr":  true,
		"ResolveUnixAddr": true,
	}
	bannedValues := map[string]bool{
		"Dialer":          true,
		"DefaultResolver": true,
		"Resolver":        true,
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		path := filepath.Join(".", name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "net" {
				return true
			}
			if banned[selector.Sel.Name] || bannedValues[selector.Sel.Name] {
				t.Errorf("%s references net.%s; proxy traffic must go through Tunnel", path, selector.Sel.Name)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no source files inspected")
	}
}
