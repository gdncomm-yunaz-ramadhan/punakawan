// Package archcheck is a build-time architecture gate: it fails go test
// if any package imports the removed MySQL/Dolt/Bleve dependencies, or
// launches an external dolt/mysqld process.
package archcheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// excludedDirs hold no Go runtime code (VCS metadata, JS/TS packages,
// vendored tool state, build output) - walking into them wastes time and
// risks matching unrelated text in generated or third-party files.
var excludedDirs = map[string]bool{
	".git": true, ".github": true, "node_modules": true, "dist": true,
	".beads": true, "packages": true, ".punakawan": true, ".serena": true,
	".codex": true, ".claude": true, "docs": true, "prompts": true,
}

// bannedImportSubstrings match the real upstream module paths for the
// dependencies this project removed.
var bannedImportSubstrings = []string{
	"dolthub/dolt",
	"dolthub/go-mysql-server",
	"go-sql-driver/mysql",
	"blevesearch/bleve",
}

// bannedProcessLiterals name external server binaries this project no
// longer launches.
var bannedProcessLiterals = map[string]bool{"dolt": true, "mysqld": true}

func TestNoLegacyRuntimeImportsOrProcessLaunches(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	var importViolations, processViolations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("archcheck: parse %s: %w", rel, err)
		}

		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range bannedImportSubstrings {
				if strings.Contains(p, banned) {
					importViolations = append(importViolations, fmt.Sprintf("%s imports %s", rel, p))
				}
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if lit := bannedProcessArg(node); lit != "" {
					processViolations = append(processViolations, fmt.Sprintf("%s: launches process %q", rel, lit))
				}
			case *ast.CompositeLit:
				if lit := bannedProcessSpecField(node); lit != "" {
					processViolations = append(processViolations, fmt.Sprintf("%s: builds process spec for %q", rel, lit))
				}
			}
			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("archcheck: walk repo: %v", err)
	}

	if len(importViolations) > 0 {
		t.Errorf("legacy runtime imports found:\n%s", strings.Join(importViolations, "\n"))
	}
	if len(processViolations) > 0 {
		t.Errorf("unapproved dolt/mysqld process launch:\n%s", strings.Join(processViolations, "\n"))
	}
}

// bannedProcessArg reports a banned literal ("dolt" or "mysqld") passed
// as the first argument to exec.Command/exec.CommandContext, or "" if
// the call is not one of those or its first argument is not a matching
// string literal. exec.LookPath is deliberately not matched here - it
// only checks PATH for a binary's presence and never starts a process.
func bannedProcessArg(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "exec" {
		return ""
	}
	if sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" {
		return ""
	}
	argIdx := 0
	if sel.Sel.Name == "CommandContext" {
		argIdx = 1 // exec.CommandContext(ctx, name, args...)
	}
	if len(call.Args) <= argIdx {
		return ""
	}
	return bannedStringLit(call.Args[argIdx])
}

// bannedProcessSpecField reports a banned literal assigned to a Name:
// field in a struct literal (the shape internal/tools.Spec uses),
// covering supervised launches that never call exec.Command directly.
func bannedProcessSpecField(lit *ast.CompositeLit) string {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Name" {
			continue
		}
		if found := bannedStringLit(kv.Value); found != "" {
			return found
		}
	}
	return ""
}

func bannedStringLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	v := strings.Trim(lit.Value, `"`)
	if bannedProcessLiterals[v] {
		return v
	}
	return ""
}

// repoRoot walks up from the test's working directory to find the
// module root, so the check works whether it's run from the repo root
// or from within this package's own directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("archcheck: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("archcheck: could not locate repo root (go.mod not found)")
		}
		dir = parent
	}
}
