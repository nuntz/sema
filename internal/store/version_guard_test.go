package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// change records only the writes made through the Store it hands its
// closure, so each closure names its parameter after the enclosing receiver:
// the shadowing leaves the outer Store unreachable, and a write through it
// would skip the bump silently. bumpVersion is called only from change, so no
// writer re-derives when to bump.
func TestVersionBumpsGoThroughChange(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	changes := 0
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
			receiver := ""
			if function.Recv != nil && len(function.Recv.List[0].Names) == 1 {
				receiver = function.Recv.List[0].Names[0].Name
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch selector.Sel.Name {
				case "bumpVersion":
					if function.Name.Name != "change" {
						t.Errorf("%s: %s calls bumpVersion; bump through change", files.Position(call.Pos()), function.Name.Name)
					}
				case "change":
					changes++
					literal, ok := call.Args[len(call.Args)-1].(*ast.FuncLit)
					params := []*ast.Field{}
					if ok {
						params = literal.Type.Params.List
					}
					if len(params) != 1 || len(params[0].Names) != 1 || params[0].Names[0].Name != receiver {
						t.Errorf("%s: %s passes change a closure that doesn't shadow its receiver %q", files.Position(call.Pos()), function.Name.Name, receiver)
					}
				}
				return true
			})
		}
	}
	// SaveFeeds, DeleteFeed, SetRead, SetSignal and SetHeart.
	if changes < 5 {
		t.Fatalf("found %d calls to change, want at least 5; is the guard still reading the writers?", changes)
	}
}
