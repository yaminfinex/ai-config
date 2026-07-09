#!/usr/bin/env bash
# check-updatelocked-errors.sh — forbid discarded registry.UpdateLocked errors.
#
# The scanner is intentionally strict about the error result: UpdateLocked may
# be returned, passed as arguments to another call, or assigned to an identifier
# whose binding is checked before being overwritten. Assigning the error result
# into a selector such as b.err is rejected; use a local err binding and copy it
# after checking if that shape is needed. Without type information, selector
# aliases and interface methods named UpdateLocked are not resolved. If-init
# checks intentionally do not model overwrite-before-check inside the branch.
# Reads that occur only inside nested closures do not count as checks; this is
# conservative by design because deferred/goroutine captures are easy to mistake
# for durable handling of a registry write failure.

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
	parents  map[ast.Node]ast.Node
	aliases  map[string]bool
	aliasObj map[*ast.Object]bool
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
		aliasObj: map[*ast.Object]bool{},
		parents: map[ast.Node]ast.Node{},
	}
	s.indexParents()
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
	s.collectFunctionAliases()
	s.scanCalls()
	return s.bad, nil
}

func (s *scanner) indexParents() {
	var stack []ast.Node
	ast.Inspect(s.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			s.parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
}

func (s *scanner) collectFunctionAliases() {
	ast.Inspect(s.file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				if i >= len(node.Lhs) || !s.isUpdateLockedValue(rhs) {
					continue
				}
				if id, ok := node.Lhs[i].(*ast.Ident); ok && id.Name != "_" {
					s.aliases[id.Name] = true
					if id.Obj != nil {
						s.aliasObj[id.Obj] = true
					}
				}
			}
		case *ast.ValueSpec:
			for i, rhs := range node.Values {
				if i >= len(node.Names) || !s.isUpdateLockedValue(rhs) {
					continue
				}
				id := node.Names[i]
				if id.Name != "_" {
					s.aliases[id.Name] = true
					if id.Obj != nil {
						s.aliasObj[id.Obj] = true
					}
				}
			}
		}
		return true
	})
}

func (s *scanner) scanCalls() {
	ast.Inspect(s.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !s.isUpdateLockedCall(call) {
			return true
		}
		s.checkCall(call)
		return true
	})
}

func (s *scanner) checkCall(call *ast.CallExpr) {
	parent, child := s.contextParent(call)
	switch p := parent.(type) {
	case *ast.AssignStmt:
		if exprListContains(p.Rhs, child) {
			s.checkAssign(p, child)
			return
		}
	case *ast.ValueSpec:
		if exprListContains(p.Values, child) {
			s.checkValueSpec(p, child)
			return
		}
	case *ast.ReturnStmt:
		if exprListContains(p.Results, child) {
			return
		}
	case *ast.CallExpr:
		if p.Fun != child && exprListContains(p.Args, child) {
			return
		}
	}
	s.report(call.Pos(), "registry.UpdateLocked call discards the error")
}

func (s *scanner) checkAssign(as *ast.AssignStmt, callExpr ast.Expr) {
	if len(as.Rhs) != 1 || unwrapParen(as.Rhs[0]) != callExpr {
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
	if s.initBindingChecked(errIdent, as) {
		return
	}
	if !s.errorBindingChecked(errIdent, as) {
		s.report(errIdent.Pos(), "registry.UpdateLocked error result is assigned but never checked")
	}
}

func (s *scanner) checkValueSpec(vs *ast.ValueSpec, callExpr ast.Expr) {
	if len(vs.Values) != 1 || unwrapParen(vs.Values[0]) != callExpr {
		return
	}
	if len(vs.Names) < 2 {
		s.report(vs.Pos(), "registry.UpdateLocked call does not bind the error result")
		return
	}
	errIdent := vs.Names[1]
	if errIdent.Name == "_" {
		s.report(errIdent.Pos(), "registry.UpdateLocked error result is assigned to _")
		return
	}
	if s.nearestFunc(vs) == nil || !s.errorBindingChecked(errIdent, vs) {
		s.report(errIdent.Pos(), "registry.UpdateLocked error result is assigned but never checked")
	}
}

func (s *scanner) isUpdateLockedCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	return s.isUpdateLockedValue(call.Fun)
}

func (s *scanner) isUpdateLockedValue(expr ast.Expr) bool {
	switch fn := unwrapParen(expr).(type) {
	case *ast.Ident:
		if fn.Name == "UpdateLocked" && (s.file.Name.Name == "registry" || s.dotAlias) {
			return true
		}
		if fn.Obj != nil {
			return s.aliasObj[fn.Obj]
		}
		return s.aliases[fn.Name]
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

func (s *scanner) contextParent(expr ast.Expr) (ast.Node, ast.Expr) {
	child := expr
	parent := s.parents[child]
	for {
		if _, ok := parent.(*ast.ParenExpr); !ok {
			return parent, child
		}
		child = parent.(ast.Expr)
		parent = s.parents[parent]
	}
}

func (s *scanner) errorBindingChecked(errIdent *ast.Ident, assigned ast.Node) bool {
	if errIdent.Obj == nil {
		return false
	}
	root := s.nearestFunc(assigned)
	if root == nil {
		return false
	}
	readStart := assigned.End()
	stopStart := assigned.End()
	if branch := s.enclosingMutualExclusion(assigned); branch != nil {
		stopStart = branch.End()
	}
	var stop ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || n.Pos() <= stopStart {
			return true
		}
		if fl, ok := n.(*ast.FuncLit); ok && fl != root {
			return false
		}
		if n != assigned && s.nodeAssignsBinding(n, errIdent) && (stop == nil || n.Pos() < stop.Pos()) {
			stop = n
			return false
		}
		return true
	})
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found || n == nil || n.Pos() <= readStart {
			return !found
		}
		if stop != nil && n.Pos() >= stop.End() {
			return false
		}
		if fl, ok := n.(*ast.FuncLit); ok && fl != root {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || !sameBinding(id, errIdent) || s.isWriteIdent(id) {
			return true
		}
		found = true
		return false
	})
	return found
}

func (s *scanner) initBindingChecked(errIdent *ast.Ident, assigned *ast.AssignStmt) bool {
	switch p := s.parents[assigned].(type) {
	case *ast.IfStmt:
		return s.bindingReadInNode(errIdent, p.Cond) || s.bindingReadInNode(errIdent, p.Body) || s.bindingReadInNode(errIdent, p.Else)
	case *ast.ForStmt:
		return s.bindingReadInNode(errIdent, p.Cond) || s.bindingReadInNode(errIdent, p.Post) || s.bindingReadInNode(errIdent, p.Body)
	case *ast.SwitchStmt:
		return s.bindingReadInNode(errIdent, p.Tag) || s.bindingReadInNode(errIdent, p.Body)
	case *ast.TypeSwitchStmt:
		return s.bindingReadInNode(errIdent, p.Body)
	}
	return false
}

func (s *scanner) bindingReadInNode(target *ast.Ident, node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found || n == nil {
			return !found
		}
		if fl, ok := n.(*ast.FuncLit); ok {
			return fl == node
		}
		id, ok := n.(*ast.Ident)
		if !ok || !sameBinding(id, target) || s.isWriteIdent(id) {
			return true
		}
		found = true
		return false
	})
	return found
}

func (s *scanner) nodeAssignsBinding(n ast.Node, target *ast.Ident) bool {
	switch node := n.(type) {
	case *ast.AssignStmt:
		for _, lhs := range node.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && sameBinding(id, target) {
				return true
			}
		}
	case *ast.ValueSpec:
		for _, id := range node.Names {
			if sameBinding(id, target) {
				return true
			}
		}
	case *ast.RangeStmt:
		if id, ok := node.Key.(*ast.Ident); ok && sameBinding(id, target) {
			return true
		}
		if id, ok := node.Value.(*ast.Ident); ok && sameBinding(id, target) {
			return true
		}
	}
	return false
}

func (s *scanner) isWriteIdent(id *ast.Ident) bool {
	parent := s.parents[id]
	switch p := parent.(type) {
	case *ast.AssignStmt:
		return identInExprList(id, p.Lhs)
	case *ast.ValueSpec:
		for _, name := range p.Names {
			if name == id {
				return true
			}
		}
	case *ast.RangeStmt:
		return p.Key == id || p.Value == id
	}
	return false
}

func (s *scanner) nearestFunc(n ast.Node) ast.Node {
	for p := n; p != nil; p = s.parents[p] {
		switch p.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return p
		}
	}
	return nil
}

func (s *scanner) enclosingMutualExclusion(n ast.Node) ast.Node {
	for p := s.parents[n]; p != nil; p = s.parents[p] {
		if ifs, ok := p.(*ast.IfStmt); ok && n.Pos() > ifs.Body.Pos() && n.End() <= ifs.Body.End() {
			return ifs
		}
		if ifs, ok := p.(*ast.IfStmt); ok && ifs.Else != nil && n.Pos() >= ifs.Else.Pos() && n.End() <= ifs.Else.End() {
			return ifs
		}
		if _, ok := p.(*ast.CaseClause); ok {
			if owner := s.caseOwner(p); owner != nil {
				return owner
			}
		}
		if _, ok := p.(*ast.CommClause); ok {
			if owner := s.commOwner(p); owner != nil {
				return owner
			}
		}
	}
	return nil
}

func (s *scanner) caseOwner(cc ast.Node) ast.Node {
	for p := s.parents[cc]; p != nil; p = s.parents[p] {
		switch p.(type) {
		case *ast.SwitchStmt, *ast.TypeSwitchStmt:
			return p
		}
	}
	return nil
}

func (s *scanner) commOwner(cc ast.Node) ast.Node {
	for p := s.parents[cc]; p != nil; p = s.parents[p] {
		if _, ok := p.(*ast.SelectStmt); ok {
			return p
		}
	}
	return nil
}

func (s *scanner) report(pos token.Pos, reason string) {
	p := s.fset.Position(pos)
	line := ""
	if p.Line > 0 && p.Line <= len(s.lines) {
		line = strings.TrimSpace(s.lines[p.Line-1])
	}
	s.bad = append(s.bad, fmt.Sprintf("%s:%d: %s: %s", s.path, p.Line, reason, line))
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

func unwrapParen(expr ast.Expr) ast.Expr {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = p.X
	}
}

func exprListContains(list []ast.Expr, target ast.Expr) bool {
	for _, expr := range list {
		if unwrapParen(expr) == target {
			return true
		}
	}
	return false
}

func identInExprList(id *ast.Ident, list []ast.Expr) bool {
	for _, expr := range list {
		if expr == id {
			return true
		}
	}
	return false
}

func sameBinding(a, b *ast.Ident) bool {
	return a != nil && b != nil && a.Obj != nil && a.Obj == b.Obj
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
mkdir -p "$neg_root"/{blank,bare,ignored,defer,go,decl_func,decl_package,labeled,switch_init,funclit,method_value,paren,shadow,field_use,selector_assign,guarded_unchecked,overwrite_unread,switch_unchecked}

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

cat >"$neg_root/defer/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func deferredDiscard() {
	defer registry.UpdateLocked("registry.jsonl", nil)
}
GO

cat >"$neg_root/go/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func goroutineDiscard() {
	go registry.UpdateLocked("registry.jsonl", nil)
}
GO

cat >"$neg_root/decl_func/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func varDiscard() {
	var _, _ = registry.UpdateLocked("registry.jsonl", nil)
}
GO

cat >"$neg_root/decl_package/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

var _, _ = registry.UpdateLocked("registry.jsonl", nil)
GO

cat >"$neg_root/labeled/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func labeledDiscard() {
label:
	registry.UpdateLocked("registry.jsonl", nil)
}
GO

cat >"$neg_root/switch_init/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func switchInitDiscard() {
	switch _, _ = registry.UpdateLocked("registry.jsonl", nil); true {
	case true:
	}
}
GO

cat >"$neg_root/funclit/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func closureDiscard() {
	go func() {
		_, _ = registry.UpdateLocked("registry.jsonl", nil)
	}()
}
GO

cat >"$neg_root/method_value/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func methodValueDiscard() {
	update := registry.UpdateLocked
	update("registry.jsonl", nil)
}
GO

cat >"$neg_root/paren/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func parenDiscard() {
	(registry.UpdateLocked)("registry.jsonl", nil)
}
GO

cat >"$neg_root/shadow/negative.go" <<'GO'
package negative

import (
	"os"
	registry "ai-config/tools/herder/internal/registry"
)

func shadowedErr(path string) error {
	_, err := registry.UpdateLocked("registry.jsonl", nil)
	data, err := os.ReadFile(path)
	_ = data
	if err != nil {
		return err
	}
	return nil
}
GO

cat >"$neg_root/field_use/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

var holder struct{ err error }

func fieldUseIsNotCheck() {
	_, err := registry.UpdateLocked("registry.jsonl", nil)
	holder.err = nil
}
GO

cat >"$neg_root/selector_assign/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

var holder struct{ err error }

func selectorAssignIsRejected() {
	_, holder.err = registry.UpdateLocked("registry.jsonl", nil)
	if holder.err != nil {
		return
	}
}
GO

cat >"$neg_root/guarded_unchecked/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func guardedUnchecked(cond bool) error {
	if cond {
		_, err := registry.UpdateLocked("registry.jsonl", nil)
		_ = "not the error"
		_ = "still not the error"
	}
	return nil
}
GO

cat >"$neg_root/overwrite_unread/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func other() error { return nil }

func overwrittenBeforeRead() error {
	_, err := registry.UpdateLocked("registry.jsonl", nil)
	err = other()
	return err
}
GO

cat >"$neg_root/switch_unchecked/negative.go" <<'GO'
package negative

import registry "ai-config/tools/herder/internal/registry"

func switchUnchecked(mode string) error {
	var err error
	switch mode {
	case "a":
		_, err = registry.UpdateLocked("a.jsonl", nil)
	case "b":
		_, err = registry.UpdateLocked("b.jsonl", nil)
	}
	return nil
}
GO

expect_violation() {
  local name="$1" want="$2" rc
  set +e
  updatelocked_gate "$neg_root/$name" >"$ROOT/negative-$name.out" 2>&1
  rc=$?
  set -e
  if [[ "$rc" != "1" ]]; then
    fail_case "UpdateLocked error gate negative demo catches $name" "want exit 1, got $rc: $(cat "$ROOT/negative-$name.out")"
  elif ! grep -q "$want" "$ROOT/negative-$name.out"; then
    fail_case "UpdateLocked error gate negative demo catches $name" "missing expected diagnostic $want: $(cat "$ROOT/negative-$name.out")"
  else
    pass "UpdateLocked error gate negative demo catches $name"
  fi
}

expect_violation blank 'registry.UpdateLocked error result is assigned to _'
expect_violation bare 'registry.UpdateLocked call discards the error'
expect_violation ignored 'registry.UpdateLocked error result is assigned but never checked'
expect_violation defer 'registry.UpdateLocked call discards the error'
expect_violation go 'registry.UpdateLocked call discards the error'
expect_violation decl_func 'registry.UpdateLocked error result is assigned to _'
expect_violation decl_package 'registry.UpdateLocked error result is assigned to _'
expect_violation labeled 'registry.UpdateLocked call discards the error'
expect_violation switch_init 'registry.UpdateLocked error result is assigned to _'
expect_violation funclit 'registry.UpdateLocked error result is assigned to _'
expect_violation method_value 'registry.UpdateLocked call discards the error'
expect_violation paren 'registry.UpdateLocked call discards the error'
expect_violation shadow 'registry.UpdateLocked error result is assigned but never checked'
expect_violation field_use 'registry.UpdateLocked error result is assigned but never checked'
expect_violation selector_assign 'registry.UpdateLocked error result is not assigned to a named error'
expect_violation guarded_unchecked 'registry.UpdateLocked error result is assigned but never checked'
expect_violation overwrite_unread 'registry.UpdateLocked error result is assigned but never checked'
expect_violation switch_unchecked 'registry.UpdateLocked error result is assigned but never checked'

pos_root="$ROOT/positive"
mkdir -p "$pos_root"/{branch_checked,return_forward,argument_forward,guarded_branch,error_wrap,switch_cases,select_cases}

cat >"$pos_root/branch_checked/positive.go" <<'GO'
package positive

import registry "ai-config/tools/herder/internal/registry"

func branchChecked(cond bool) error {
	var err error
	if cond {
		_, err = registry.UpdateLocked("a.jsonl", nil)
	} else {
		_, err = registry.UpdateLocked("b.jsonl", nil)
	}
	return err
}
GO

cat >"$pos_root/return_forward/positive.go" <<'GO'
package positive

import registry "ai-config/tools/herder/internal/registry"

func returnForward() ([][]byte, error) {
	return registry.UpdateLocked("registry.jsonl", nil)
}
GO

cat >"$pos_root/argument_forward/positive.go" <<'GO'
package positive

import registry "ai-config/tools/herder/internal/registry"

func use(_ [][]byte, _ error) {}

func argumentForward() {
	use(registry.UpdateLocked("registry.jsonl", nil))
}
GO

cat >"$pos_root/guarded_branch/positive.go" <<'GO'
package positive

import registry "ai-config/tools/herder/internal/registry"

func guardedBranch(cond bool) error {
	if cond {
		_, err := registry.UpdateLocked("registry.jsonl", nil)
		if err != nil {
			return err
		}
	}
	return nil
}
GO

cat >"$pos_root/error_wrap/positive.go" <<'GO'
package positive

import (
	"fmt"
	registry "ai-config/tools/herder/internal/registry"
)

func errorWrap() error {
	_, err := registry.UpdateLocked("registry.jsonl", nil)
	err = fmt.Errorf("update registry: %w", err)
	return err
}
GO

cat >"$pos_root/switch_cases/positive.go" <<'GO'
package positive

import registry "ai-config/tools/herder/internal/registry"

func switchCases(mode string) error {
	var err error
	switch mode {
	case "a":
		_, err = registry.UpdateLocked("a.jsonl", nil)
	case "b":
		_, err = registry.UpdateLocked("b.jsonl", nil)
	}
	return err
}
GO

cat >"$pos_root/select_cases/positive.go" <<'GO'
package positive

import registry "ai-config/tools/herder/internal/registry"

func selectCases(a, b <-chan struct{}) error {
	var err error
	select {
	case <-a:
		_, err = registry.UpdateLocked("a.jsonl", nil)
	case <-b:
		_, err = registry.UpdateLocked("b.jsonl", nil)
	}
	return err
}
GO

for name in branch_checked return_forward argument_forward guarded_branch error_wrap switch_cases select_cases; do
  if updatelocked_gate "$pos_root/$name" >"$ROOT/positive-$name.out" 2>&1; then
    pass "UpdateLocked error gate positive demo allows $name"
  else
    fail_case "UpdateLocked error gate positive demo allows $name" "$(cat "$ROOT/positive-$name.out")"
  fi
done

exit "$fail"
