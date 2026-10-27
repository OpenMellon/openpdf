// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type finding struct {
	pos    token.Position
	method string
}

func (f finding) String() string {
	return fmt.Sprintf("%s:%d:%d: unguarded common.Log.%s in for-loop body", f.pos.Filename, f.pos.Line, f.pos.Column, f.method)
}
func (c *fileCtx) hasName(name string) bool {
	for _, n := range c.names {
		if n == name {
			return true
		}
	}
	return false
}

var levelConsts = map[string]string{"Trace": "LogLevelTrace", "Debug": "LogLevelDebug"}

const commonImportPath = "github.com/OpenMellon/openpdf/common"

func inLoopBody(stack []ast.Node) bool {
	for i := 0; i < len(stack)-1; i++ {
		var body ast.Node
		switch stmt := stack[i].(type) {
		case *ast.ForStmt:
			body = stmt.Body
		case *ast.RangeStmt:
			body = stmt.Body
		default:
			continue
		}
		if stack[i+1] == body {
			return true
		}
	}
	return false
}
func newFileCtx(file *ast.File) fileCtx {
	var ctx fileCtx
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != commonImportPath {
			continue
		}
		switch {
		case imp.Name == nil:
			ctx.names = append(ctx.names, "common")
		case imp.Name.Name == ".":
			ctx.dotImport = true
		case imp.Name.Name == "_":
		default:
			ctx.names = append(ctx.names, imp.Name.Name)
		}
	}
	return ctx
}
func logCallMethod(call *ast.CallExpr, ctx *fileCtx) (string, bool) {
	if len(call.Args) < 2 {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	method := sel.Sel.Name
	if _, known := levelConsts[method]; !known {
		return "", false
	}
	if !isCommonLog(sel.X, ctx) {
		return "", false
	}
	return method, true
}
func checkFile(fset *token.FileSet, file *ast.File) []finding {
	ctx := newFileCtx(file)
	if len(ctx.names) == 0 && !ctx.dotImport {
		return nil
	}
	var (
		stack    []ast.Node
		findings []finding
	)
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := logCallMethod(call, &ctx)
		if !ok {
			return true
		}
		if !inLoopBody(stack) || guardedByLevelCheck(stack, &ctx, method) {
			return true
		}
		findings = append(findings, finding{pos: fset.Position(call.Pos()), method: method})
		return true
	})
	return findings
}
func main() {
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	fset := token.NewFileSet()
	var (
		findings    []finding
		parseErrors int
	)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: parse error: %v\n", path, err)
			parseErrors++
			return nil
		}
		findings = append(findings, checkFile(fset, file)...)
		return nil
	})
	if walkErr != nil {
		fmt.Fprintln(os.Stderr, walkErr)
		os.Exit(2)
	}
	for _, f := range findings {
		fmt.Println(f.String())
	}
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d unguarded hot-loop common.Log.Trace/Debug call(s) found\n", len(findings))
	}
	if parseErrors > 0 {
		fmt.Fprintf(os.Stderr, "%d file(s) could not be parsed; results may be incomplete\n", parseErrors)
	}
	if len(findings) > 0 || parseErrors > 0 {
		os.Exit(1)
	}
}
func isCommonLog(expr ast.Expr, ctx *fileCtx) bool {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if e.Sel.Name != "Log" {
			return false
		}
		id, ok := e.X.(*ast.Ident)
		return ok && ctx.hasName(id.Name)
	case *ast.Ident:
		return ctx.dotImport && e.Name == "Log"
	}
	return false
}

var skipDirs = map[string]bool{"testdata": true, "vendor": true, ".git": true, ".github": true, "third_party": true, "examples": true, "e2etest": true, "testutils": true}

func guardedByLevelCheck(stack []ast.Node, ctx *fileCtx, method string) bool {
	level := levelConsts[method]
	if level == "" {
		return false
	}
	for i := 0; i < len(stack)-1; i++ {
		ifStmt, ok := stack[i].(*ast.IfStmt)
		if !ok {
			continue
		}
		checked, ok := levelCheckArg(ifStmt.Cond, ctx)
		if !ok || checked != level {
			continue
		}
		if stack[i+1] == ast.Node(ifStmt.Body) {
			return true
		}
	}
	return false
}

type fileCtx struct {
	names     []string
	dotImport bool
}

func levelCheckArg(cond ast.Expr, ctx *fileCtx) (string, bool) {
	call, ok := cond.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "IsLogLevel" {
		return "", false
	}
	if !isCommonLog(sel.X, ctx) {
		return "", false
	}
	switch arg := call.Args[0].(type) {
	case *ast.SelectorExpr:
		id, ok := arg.X.(*ast.Ident)
		if !ok || !ctx.hasName(id.Name) {
			return "", false
		}
		return arg.Sel.Name, true
	case *ast.Ident:
		if !ctx.dotImport {
			return "", false
		}
		return arg.Name, true
	}
	return "", false
}
