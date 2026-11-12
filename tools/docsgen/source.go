package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
)

type source struct {
	name   string
	fset   *token.FileSet
	file   *ast.File
	consts map[string]constant.Value
}

func parseSource(src fs.FS, name string) (*source, error) {
	data, err := fs.ReadFile(src, name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, data, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	s := &source{name: name, fset: fset, file: file, consts: map[string]constant.Value{}}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		s.addConsts(gen)
	}

	return s, nil
}

func (s *source) addConsts(gen *ast.GenDecl) {
	for _, spec := range gen.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok || len(valueSpec.Values) != len(valueSpec.Names) {
			continue
		}
		for i, name := range valueSpec.Names {
			if value, ok := s.fold(valueSpec.Values[i]); ok {
				s.consts[name.Name] = value
			}
		}
	}
}

func (s *source) fold(expr ast.Expr) (constant.Value, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		value := constant.MakeFromLiteral(e.Value, e.Kind, 0)

		return value, value.Kind() != constant.Unknown
	case *ast.Ident:
		if e.Name == "true" || e.Name == "false" {
			return constant.MakeBool(e.Name == "true"), true
		}
		value, ok := s.consts[e.Name]

		return value, ok
	case *ast.ParenExpr:
		return s.fold(e.X)
	case *ast.BinaryExpr:
		x, okX := s.fold(e.X)
		y, okY := s.fold(e.Y)
		concat := okX && okY && e.Op == token.ADD
		if !concat || x.Kind() != constant.String || y.Kind() != constant.String {
			return nil, false
		}

		return constant.BinaryOp(x, token.ADD, y), true
	default:
		return nil, false
	}
}

func (s *source) str(expr ast.Expr) (string, bool) {
	value, ok := s.fold(expr)
	if !ok || value.Kind() != constant.String {
		return "", false
	}

	return constant.StringVal(value), true
}

func (s *source) errorf(node ast.Node, format string, args ...any) error {
	return fmt.Errorf("%s: %s", s.fset.Position(node.Pos()), fmt.Sprintf(format, args...))
}

func (s *source) importName(want, fallback string) (string, bool) {
	for _, imp := range s.file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || (path != want && !strings.HasSuffix(path, "/"+want)) {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name, true
		}

		return fallback, true
	}

	return "", false
}

func selectorPath(expr ast.Expr) (root string, path []string, ok bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name, nil, true
	case *ast.SelectorExpr:
		root, path, ok = selectorPath(e.X)

		return root, append(path, e.Sel.Name), ok
	default:
		return "", nil, false
	}
}

func isPackageCall(expr ast.Expr, pkg, name string) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return nil, false
	}
	x, ok := sel.X.(*ast.Ident)

	return call, ok && x.Name == pkg
}
