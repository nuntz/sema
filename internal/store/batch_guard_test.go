package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// BatchGetItem reports throttled keys as UnprocessedKeys, not as an error,
// so the SDK never backs them off. batchGet is the one loop that waits
// between retries; a lookup that calls BatchGetItem itself retries into the
// same hot partition at once (#73).
func TestBatchGetsGoThroughBatchGet(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	calls := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "BatchGetItem" {
					return true
				}
				calls++
				if function.Name.Name != "batchGet" {
					t.Errorf("%s: %s calls BatchGetItem; read through batchGet, which backs off throttled keys", files.Position(call.Pos()), function.Name.Name)
				}
				return true
			})
		}
	}
	if calls == 0 {
		t.Fatal("found no BatchGetItem calls; is the guard still reading the store?")
	}
}
