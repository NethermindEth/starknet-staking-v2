package main

import (
	"errors"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

type option struct {
	name      string
	shorthand string
	envVar    string
	jsonKey   string
	value     string
	usage     string
}

type override struct {
	registered []string
	shown      string
}

func defaultOverrides() map[string]override {
	const autoDetected = "auto"

	return map[string]override{
		"attest-contract-address":  {registered: []string{`""`}, shown: autoDetected},
		"staking-contract-address": {registered: []string{`""`}, shown: autoDetected},
		"reward-claim-max-fee":     {registered: []string{"0"}, shown: "Unlimited"},
		"max-tip":                  {registered: []string{"0"}, shown: "Unlimited"},
		"reward-claim-threshold":   {registered: []string{"0"}, shown: "-"},
		"log-level": {
			registered: []string{"log.INFO.String()", "utils.INFO.String()"},
			shown:      "`info`",
		},
	}
}

type commandModel struct {
	src       *source
	config    *configModel
	configVar string
	copies    map[string][]string
	overrides map[string]override
}

func parseOptions(src *source, config *configModel) ([]option, error) {
	configPkg, ok := src.importName("validator/config", "config")
	if !ok {
		return nil, errors.New(src.name + ": does not import validator/config")
	}
	configVar, err := findConfigVar(src, configPkg)
	if err != nil {
		return nil, err
	}
	m := &commandModel{
		src:       src,
		config:    config,
		configVar: configVar,
		copies:    findCopies(src, configVar),
		overrides: defaultOverrides(),
	}

	var options []option
	seen := map[string]bool{}
	ast.Inspect(src.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || err != nil {
			return err == nil
		}
		var opt *option
		opt, err = m.option(call)
		if opt == nil || err != nil {
			return err == nil
		}
		if seen[opt.name] {
			err = src.errorf(call, "--%s is registered twice", opt.name)

			return false
		}
		seen[opt.name] = true
		options = append(options, *opt)

		return true
	})
	if err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return nil, errors.New(src.name + ": no flag registrations found")
	}

	return options, nil
}

func findConfigVar(src *source, configPkg string) (string, error) {
	var names []string
	ast.Inspect(src.file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		root, path, ok := selectorPath(spec.Type)
		if ok && root == configPkg && slices.Equal(path, []string{rootConfigType}) {
			for _, name := range spec.Names {
				names = append(names, name.Name)
			}
		}

		return true
	})
	if len(names) != 1 {
		return "", errors.New(
			src.name + ": want one variable of type " + configPkg + ".Config, found " +
				strconv.Itoa(len(names)),
		)
	}

	return names[0], nil
}

func findCopies(src *source, configVar string) map[string][]string {
	copies := map[string][]string{}
	ast.Inspect(src.file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		root, path, ok := selectorPath(assign.Lhs[0])
		if !ok || root != configVar || len(path) == 0 {
			return true
		}
		rhs := assign.Rhs[0]
		if unary, ok := rhs.(*ast.UnaryExpr); ok && unary.Op == token.AND {
			rhs = unary.X
		}
		if ident, ok := rhs.(*ast.Ident); ok {
			joined := strings.Join(path, ".")
			if !slices.Contains(copies[ident.Name], joined) {
				copies[ident.Name] = append(copies[ident.Name], joined)
			}
		}

		return true
	})

	return copies
}

func flagSetCall(call *ast.CallExpr) (flagSet, method string, ok bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok || len(inner.Args) != 0 {
		return "", "", false
	}
	innerSel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasSuffix(innerSel.Sel.Name, "Flags") {
		return "", "", false
	}

	return innerSel.Sel.Name, sel.Sel.Name, true
}

func (m *commandModel) option(call *ast.CallExpr) (*option, error) {
	flagSet, method, ok := flagSetCall(call)
	if !ok || method == "Changed" || method == "Lookup" {
		return nil, nil
	}
	if flagSet != "Flags" {
		return nil, m.src.errorf(call, "unsupported %s().%s call", flagSet, method)
	}

	hasShorthand := strings.HasSuffix(method, "VarP")
	if !hasShorthand && !strings.HasSuffix(method, "Var") {
		return nil, m.src.errorf(call, "unsupported Flags().%s call", method)
	}
	argCount := 4
	if hasShorthand {
		argCount++
	}
	if len(call.Args) != argCount {
		return nil, m.src.errorf(
			call, "Flags().%s has %d arguments, want %d", method, len(call.Args), argCount,
		)
	}

	opt, err := m.parseOption(call.Args, hasShorthand)
	if err != nil {
		return nil, err
	}

	return &opt, nil
}

func (m *commandModel) parseOption(args []ast.Expr, hasShorthand bool) (option, error) {
	name, ok := m.src.str(args[1])
	if !ok {
		return option{}, m.src.errorf(args[1], "flag name is not a constant string")
	}
	var shorthand string
	if hasShorthand {
		if shorthand, ok = m.src.str(args[2]); !ok {
			return option{}, m.src.errorf(args[2], "--%s shorthand is not a constant string", name)
		}
	}
	usageArg := args[len(args)-1]
	usage, ok := m.src.str(usageArg)
	if !ok {
		return option{}, m.src.errorf(usageArg, "--%s usage is not a constant string", name)
	}
	value, err := m.defaultValue(name, args[len(args)-2])
	if err != nil {
		return option{}, err
	}
	field, err := m.binding(name, args[0])
	if err != nil {
		return option{}, err
	}

	return option{
		name:      name,
		shorthand: shorthand,
		envVar:    field.envVar,
		jsonKey:   field.jsonKey,
		value:     value,
		usage:     usage,
	}, nil
}

func (m *commandModel) binding(name string, target ast.Expr) (configField, error) {
	unary, ok := target.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return configField{}, m.src.errorf(target, "--%s target is not an address", name)
	}
	root, path, ok := selectorPath(unary.X)
	if !ok {
		return configField{}, m.src.errorf(target, "--%s target is not a variable", name)
	}
	if root != m.configVar || len(path) == 0 {
		copies := m.copies[root]
		switch {
		case len(path) > 0 || len(copies) == 0:
			return configField{jsonKey: "", envVar: ""}, nil
		case len(copies) > 1:
			return configField{}, m.src.errorf(
				target, "--%s target %s is copied into %s",
				name, root, strings.Join(copies, " and "),
			)
		}
		path = strings.Split(copies[0], ".")
	}

	field, err := m.config.field(path)
	if err != nil {
		return configField{}, m.src.errorf(target, "--%s: %v", name, err)
	}

	return field, nil
}

func (m *commandModel) defaultValue(name string, expr ast.Expr) (string, error) {
	registered := types.ExprString(expr)
	if o, ok := m.overrides[name]; ok {
		if !slices.Contains(o.registered, registered) {
			return "", m.src.errorf(
				expr, "--%s default %s does not match its override, review it", name, registered,
			)
		}

		return o.shown, nil
	}

	value, ok := m.src.fold(expr)
	if !ok {
		return "", m.src.errorf(
			expr, "--%s default %s cannot be evaluated, add an override", name, registered,
		)
	}
	var text string
	switch value.Kind() {
	case constant.String:
		text = constant.StringVal(value)
	case constant.Bool:
		text = strconv.FormatBool(constant.BoolVal(value))
	case constant.Float:
		text = value.String()
	default:
		text = value.ExactString()
	}
	if text == "" {
		return "-", nil
	}

	return codeSpan(text), nil
}
