package contract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestMetricDefinitionsAgree scans production Go sources across the repository.
// Module fixtures use package-private helpers and cannot all be collected from
// a shared test without exposing new production APIs. Instead this guard uses
// go/ast to compare literal name/type/unit arguments of Builder.Metric calls,
// including calls on branches not exercised by a particular fixture. Dynamic
// names/types/units (e.g. user scripts and table-driven metrics) are not covered;
// their existing module/contract-export tests still validate emitted batches.
// Test sources are excluded because they deliberately contain invalid metrics.
func TestMetricDefinitionsAgree(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	type definition struct {
		typ, unit string
		position  token.Position
	}
	definitions := map[string]definition{}
	calls := 0
	cephCalls := 0
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 5 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Metric" {
				return true
			}
			name, nameOK := literalString(call.Args[0])
			typ, typeOK := literalString(call.Args[1])
			unit, unitOK := literalString(call.Args[3])
			if !nameOK || !typeOK || !unitOK {
				return true
			}
			calls++
			if name == "system.ceph.pool.objects" {
				cephCalls++
			}
			current := definition{typ, unit, fset.Position(call.Pos())}
			if previous, exists := definitions[name]; exists && (previous.typ != typ || previous.unit != unit) {
				t.Errorf("%s: metric %q has (%s, %q); %s defines (%s, %q)",
					current.position, name, typ, unit, previous.position, previous.typ, previous.unit)
			} else {
				definitions[name] = current
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 || cephCalls < 2 {
		t.Fatalf("incomplete scan: %d metric calls, %d Ceph pool object definitions", calls, cephCalls)
	}
	if d := definitions["system.ceph.pool.objects"]; d.typ != "gauge" || d.unit != "" {
		t.Errorf("Ceph pool objects must be gauge with empty unit, got (%s, %q)", d.typ, d.unit)
	}
	t.Logf("checked %d constant metric calls and %d distinct names", calls, len(definitions))
}

func literalString(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}
