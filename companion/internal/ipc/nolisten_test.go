package ipc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStepTwoPackagesOpenNoListener guards the control plane: the host talks
// over stdin/stdout only. The data-plane SOCKS5 listener lives in the proxy
// package and is not opened here.
func TestStepTwoPackagesOpenNoListener(t *testing.T) {
	dirs := []string{".", filepath.Join("..", "..", "cmd", "geomind-companion")}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch pkg.Name {
				case "net":
					if strings.HasPrefix(selector.Sel.Name, "Listen") {
						t.Errorf("%s calls net.%s", path, selector.Sel.Name)
					}
				case "http":
					if strings.HasPrefix(selector.Sel.Name, "Listen") || selector.Sel.Name == "Serve" {
						t.Errorf("%s calls http.%s", path, selector.Sel.Name)
					}
				}
				return true
			})
		}
	}
}
