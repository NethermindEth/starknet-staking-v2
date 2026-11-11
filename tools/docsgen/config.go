package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

const rootConfigType = "Config"

type fieldKey struct {
	typeName  string
	fieldName string
}

type configModel struct {
	src     *source
	structs map[string]*ast.StructType
	envVars map[fieldKey]string
}

type configField struct {
	jsonKey string
	envVar  string
}

func newConfigModel(src *source) (*configModel, error) {
	model := &configModel{
		src:     src,
		structs: map[string]*ast.StructType{},
		envVars: map[fieldKey]string{},
	}
	ast.Inspect(src.file, func(node ast.Node) bool {
		if spec, ok := node.(*ast.TypeSpec); ok {
			if st, ok := spec.Type.(*ast.StructType); ok {
				model.structs[spec.Name.Name] = st
			}
		}

		return true
	})
	if model.structs[rootConfigType] == nil {
		return nil, errors.New(src.name + ": no Config struct")
	}

	osName, ok := src.importName("os", "os")
	if !ok {
		return nil, errors.New(src.name + ": does not import os")
	}
	for _, decl := range src.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			if err := model.addEnvVars(fn.Body, osName); err != nil {
				return nil, err
			}
		}
	}

	return model, nil
}

func (m *configModel) addEnvVars(body *ast.BlockStmt, osName string) error {
	locals := map[string]string{}
	ast.Inspect(body, func(node ast.Node) bool {
		ifStmt, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		envVar, ok := m.getenvInit(ifStmt.Init, osName)
		if !ok {
			return true
		}
		ast.Inspect(ifStmt.Body, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if ok && assign.Tok == token.ASSIGN && len(assign.Lhs) == 1 {
				if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
					locals[ident.Name] = envVar
				}
			}

			return true
		})

		return true
	})

	var err error
	ast.Inspect(body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || err != nil {
			return err == nil
		}
		typeName, ok := lit.Type.(*ast.Ident)
		if !ok || m.structs[typeName.Name] == nil {
			return true
		}
		err = m.addLiteralEnvVars(typeName.Name, lit, locals, osName)

		return err == nil
	})

	return err
}

func (m *configModel) getenvInit(init ast.Stmt, osName string) (string, bool) {
	assign, ok := init.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return "", false
	}

	return m.getenv(assign.Rhs[0], osName)
}

func (m *configModel) getenv(expr ast.Expr, osName string) (string, bool) {
	call, ok := isPackageCall(expr, osName, "Getenv")
	if !ok || len(call.Args) != 1 {
		return "", false
	}

	return m.src.str(call.Args[0])
}

func (m *configModel) addLiteralEnvVars(
	typeName string, lit *ast.CompositeLit, locals map[string]string, osName string,
) error {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		envVar, err := m.valueEnvVar(kv.Value, locals, osName)
		if err != nil {
			return err
		}
		if envVar == "" {
			continue
		}
		field := fieldKey{typeName: typeName, fieldName: key.Name}
		if previous, seen := m.envVars[field]; seen && previous != envVar {
			return m.src.errorf(
				kv, "%s.%s is filled from both %s and %s", typeName, key.Name, previous, envVar,
			)
		}
		m.envVars[field] = envVar
	}

	return nil
}

func (m *configModel) valueEnvVar(
	value ast.Expr, locals map[string]string, osName string,
) (string, error) {
	if ident, ok := value.(*ast.Ident); ok {
		return locals[ident.Name], nil
	}
	var envVars []string
	ast.Inspect(value, func(node ast.Node) bool {
		if expr, ok := node.(ast.Expr); ok {
			if envVar, ok := m.getenv(expr, osName); ok {
				envVars = append(envVars, envVar)
			}
		}

		return true
	})
	if len(envVars) > 1 {
		return "", m.src.errorf(
			value, "filled from several env vars: %s", strings.Join(envVars, ", "),
		)
	}
	if len(envVars) == 0 {
		return "", nil
	}

	return envVars[0], nil
}

func (m *configModel) field(path []string) (configField, error) {
	typeName := rootConfigType
	keys := make([]string, 0, len(path))
	for i, name := range path {
		st := m.structs[typeName]
		if st == nil {
			return configField{}, fmt.Errorf("struct %s not found in %s", typeName, m.src.name)
		}
		field := structField(st, name)
		if field == nil {
			return configField{}, fmt.Errorf("%s has no field %s", typeName, name)
		}
		key := jsonName(field)
		if key == "" {
			return configField{}, m.src.errorf(field, "%s.%s has no json tag", typeName, name)
		}
		keys = append(keys, key)
		if i == len(path)-1 {
			envVar, ok := m.envVars[fieldKey{typeName: typeName, fieldName: name}]
			if !ok {
				return configField{}, m.src.errorf(
					field, "no os.Getenv call fills %s.%s", typeName, name,
				)
			}

			return configField{jsonKey: strings.Join(keys, "."), envVar: envVar}, nil
		}
		next, ok := field.Type.(*ast.Ident)
		if !ok {
			return configField{}, m.src.errorf(field, "%s.%s is not a struct", typeName, name)
		}
		typeName = next.Name
	}

	return configField{}, errors.New("empty config path")
}

func structField(st *ast.StructType, name string) *ast.Field {
	for _, field := range st.Fields.List {
		for _, ident := range field.Names {
			if ident.Name == name {
				return field
			}
		}
	}

	return nil
}

func jsonName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return ""
	}
	name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
	if name == "-" {
		return ""
	}

	return name
}
