#!/usr/bin/env bash
# check-updatelocked-errors.sh — forbid discarded registry.UpdateLocked errors.

set -euo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

fail=0
pass() { printf 'PASS  %s\n' "$1"; }
fail_case() { printf 'FAIL  %s: %s\n' "$1" "$2"; fail=1; }

SCANNER="$ROOT/updatelocked-error-gate.go"
cat >"$SCANNER" <<'GO'
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const registryImport = "ai-config/tools/herder/internal/registry"

type scanner struct {
	fset     *token.FileSet
	file     *ast.File
	path     string
	lines    []string
	aliases  map[string]bool
	dotAlias bool
	bad      []string
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <scan-dir>\n", os.Args[0])
		os.Exit(2)
	}
	var bad []string
	root := os.Args[1]
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fileBad, err := scanFile(path)
		if err != nil {
			return err
		}
		bad = append(bad, fileBad...)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(bad) > 0 {
		for _, line := range bad {
			fmt.Println(line)
		}
		os.Exit(1)
	}
}

func scanFile(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	s := &scanner{
		fset:    fset,
		file:    file,
		path:    path,
		lines:   readLines(path),
		aliases: map[string]bool{},
	}
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		if importPath != registryImport {
			continue
		}
		if imp.Name == nil {
			s.aliases["registry"] = true
			continue
		}
		switch imp.Name.Name {
		case ".":
			s.dotAlias = true
		case "_":
		default:
			s.aliases[imp.Name.Name] = true
		}
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			s.scanBlock(fn.Body)
		}
	}
	return s.bad, nil
}

func (s *scanner) scanBlock(block *ast.BlockStmt) {
	for i, stmt := range block.List {
		s.checkStmt(stmt, block.List[i+1:])
		s.recurse(stmt)
	}
}

func (s *scanner) checkStmt(stmt ast.Stmt, following []ast.Stmt) {
	switch st := stmt.(type) {
	case *ast.ExprStmt:
		if s.isUpdateLockedCall(st.X) {
			s.report(st.Pos(), "bare registry.UpdateLocked call discards the error")
		}
	case *ast.AssignStmt:
		s.checkAssign(st, func(name string) bool {
			return referencesIdentInStmts(following, name)
		})
	case *ast.IfStmt:
		if as, ok := st.Init.(*ast.AssignStmt); ok {
			s.checkAssign(as, func(name string) bool {
				return referencesIdent(st.Cond, name) || referencesIdent(st.Body, name) || referencesIdent(st.Else, name)
			})
		}
	case *ast.ForStmt:
		if as, ok := st.Init.(*ast.AssignStmt); ok {
			s.checkAssign(as, func(name string) bool {
				return referencesIdent(st.Cond, name) || referencesIdent(st.Post, name) || referencesIdent(st.Body, name)
			})
		}
	}
}

func (s *scanner) checkAssign(as *ast.AssignStmt, usedAfter func(string) bool) {
	if len(as.Rhs) != 1 || !s.isUpdateLockedCall(as.Rhs[0]) {
		return
	}
	if len(as.Lhs) < 2 {
		s.report(as.Pos(), "registry.UpdateLocked call does not bind the error result")
		return
	}
	errIdent, ok := as.Lhs[1].(*ast.Ident)
	if !ok {
		s.report(as.Lhs[1].Pos(), "registry.UpdateLocked error result is not assigned to a named error")
		return
	}
	if errIdent.Name == "_" {
		s.report(errIdent.Pos(), "registry.UpdateLocked error result is assigned to _")
		return
	}
	if !usedAfter(errIdent.Name) {
		s.report(errIdent.Pos(), "registry.UpdateLocked error result is assigned but never checked")
	}
}

func (s *scanner) recurse(stmt ast.Stmt) {
	switch st := stmt.(type) {
	case *ast.BlockStmt:
		s.scanBlock(st)
	case *ast.IfStmt:
		if st.Body != nil {
			s.scanBlock(st.Body)
		}
		if st.Else != nil {
			if b, ok := st.Else.(*ast.BlockStmt); ok {
				s.scanBlock(b)
			} else {
				s.recurse(st.Else)
			}
		}
	case *ast.ForStmt:
		if st.Body != nil {
			s.scanBlock(st.Body)
		}
	case *ast.RangeStmt:
		if st.Body != nil {
			s.scanBlock(st.Body)
		}
	case *ast.SwitchStmt:
		for _, stmt := range st.Body.List {
			if cc, ok := stmt.(*ast.CaseClause); ok {
				s.scanBlock(&ast.BlockStmt{List: cc.Body})
			}
		}
	case *ast.TypeSwitchStmt:
		for _, stmt := range st.Body.List {
			if cc, ok := stmt.(*ast.CaseClause); ok {
				s.scanBlock(&ast.BlockStmt{List: cc.Body})
			}
		}
	case *ast.SelectStmt:
		for _, stmt := range st.Body.List {
			if cc, ok := stmt.(*ast.CommClause); ok {
				s.scanBlock(&ast.BlockStmt{List: cc.Body})
			}
		}
	}
}

func (s *scanner) isUpdateLockedCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "UpdateLocked" && (s.file.Name.Name == "registry" || s.dotAlias)
	case *ast.SelectorExpr:
		if fn.Sel.Name != "UpdateLocked" {
			return false
		}
		id, ok := fn.X.(*ast.Ident)
		return ok && s.aliases[id.Name]
	default:
		return false
	}
}

func (s *scanner) report(pos token.Pos, reason string) {
	p := s.fset.Position(pos)
	line := ""
	if p.Line > 0 && p.Line <= len(s.lines) {
		line = strings.TrimSpace(s.lines[p.Line-1])
	}
	s.bad = append(s.bad, fmt.Sprintf("%s:%d: %s: %s", s.path, p.Line, reason, line))
}

func referencesIdentInStmts(stmts []ast.Stmt, name string) bool {
	for _, stmt := range stmts {
		if referencesIdent(stmt, name) {
			return true
		}
	}
	return false
}

func referencesIdent(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found || n == nil {
			return !found
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func readLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
GO

updatelocked_gate() {
  local scan_dir="$1"
  go run "$SCANNER" "$scan_dir"
}

if updatelocked_gate "$REPO_ROOT/tools/herder" >"$ROOT/current.out" 2>&1; then
  pass "UpdateLocked error gate current tree"
else
  fail_case "UpdateLocked error gate current tree" "$(cat "$ROOT/current.out")"
fi

neg_root="$ROOT/negative"
mkdir -p "$neg_root/blank" "$neg_root/bare" "$neg_root/ignored"

cat >"$neg_root/blank/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func blankError() {
	_, _ = registry.UpdateLocked("registry.jsonl", nil)
}
GO

cat >"$neg_root/bare/negative.go" <<'GO'
package negative

import reg "ai-config/tools/herder/internal/registry"

func bareCall() {
	reg.UpdateLocked("registry.jsonl", nil)
}
GO

cat >"$neg_root/ignored/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func ignoredNamedError() {
	_, err := registry.UpdateLocked("registry.jsonl", nil)
	_ = "not the error"
}
GO

for name in blank bare ignored; do
  if updatelocked_gate "$neg_root/$name" >"$ROOT/negative-$name.out" 2>&1; then
    fail_case "UpdateLocked error gate negative demo catches $name" "synthetic violation was not detected"
  else
    pass "UpdateLocked error gate negative demo catches $name"
  fi
done

exit "$fail"
