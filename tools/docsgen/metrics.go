package main

import (
	"errors"
	"fmt"
	"go/ast"
	"strings"
)

type metric struct {
	name   string
	kind   string
	help   string
	labels []string
}

const (
	gaugeKind   = "Gauge"
	counterKind = "Counter"
)

type constructor struct {
	kind string
	vec  bool
}

func supportedConstructors() map[string]constructor {
	return map[string]constructor{
		"NewGauge":      {kind: gaugeKind, vec: false},
		"NewGaugeVec":   {kind: gaugeKind, vec: true},
		"NewCounter":    {kind: counterKind, vec: false},
		"NewCounterVec": {kind: counterKind, vec: true},
	}
}

func isMetricConstructor(name string) bool {
	for _, kind := range []string{counterKind, gaugeKind, "Histogram", "Summary", "Untyped"} {
		if strings.HasPrefix(name, "New"+kind) {
			return true
		}
	}

	return false
}

func parseMetrics(src *source) ([]metric, error) {
	prometheus, ok := src.importName("client_golang/prometheus", "prometheus")
	if !ok {
		return nil, errors.New(src.name + ": does not import prometheus")
	}
	constructors := supportedConstructors()

	var metrics []metric
	var err error
	registered := 0
	seen := map[string]bool{}
	ast.Inspect(src.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || err != nil {
			return err == nil
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "MustRegister" {
			registered += len(call.Args)

			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != prometheus || !isMetricConstructor(sel.Sel.Name) {
			return true
		}
		c, ok := constructors[sel.Sel.Name]
		if !ok {
			err = src.errorf(call, "unsupported metric constructor %s", sel.Sel.Name)

			return false
		}
		var met metric
		if met, err = parseMetric(src, call, c); err != nil {
			return false
		}
		if seen[met.name] {
			err = src.errorf(call, "metric %s is declared twice", met.name)

			return false
		}
		seen[met.name] = true
		metrics = append(metrics, met)

		return true
	})
	if err != nil {
		return nil, err
	}
	if len(metrics) == 0 {
		return nil, errors.New(src.name + ": no metrics found")
	}
	if registered != len(metrics) {
		return nil, fmt.Errorf(
			"%s: %d metrics declared but %d passed to MustRegister",
			src.name, len(metrics), registered,
		)
	}

	return metrics, nil
}

func parseMetric(src *source, call *ast.CallExpr, c constructor) (metric, error) {
	argCount := 1
	if c.vec {
		argCount++
	}
	if len(call.Args) != argCount {
		return metric{}, src.errorf(call, "%d arguments, want %d", len(call.Args), argCount)
	}
	met, err := parseOpts(src, call.Args[0])
	if err != nil {
		return metric{}, err
	}
	met.kind = c.kind
	if c.vec {
		if met.labels, err = parseLabels(src, call.Args[1]); err != nil {
			return metric{}, err
		}
	}

	return met, nil
}

func parseOpts(src *source, expr ast.Expr) (metric, error) {
	if unary, ok := expr.(*ast.UnaryExpr); ok {
		expr = unary.X
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return metric{}, src.errorf(expr, "metric options are not a composite literal")
	}
	var met metric
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return metric{}, src.errorf(elt, "metric option without a field name")
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return metric{}, src.errorf(elt, "metric option without a field name")
		}
		value, ok := src.str(kv.Value)
		if !ok {
			return metric{}, src.errorf(kv.Value, "metric %s is not a constant string", key.Name)
		}
		switch key.Name {
		case "Name":
			met.name = value
		case "Help":
			met.help = value
		default:
			return metric{}, src.errorf(kv, "unsupported metric option %s", key.Name)
		}
	}
	if met.name == "" || met.help == "" {
		return metric{}, src.errorf(lit, "metric without a Name or Help")
	}

	return met, nil
}

func parseLabels(src *source, expr ast.Expr) ([]string, error) {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, src.errorf(expr, "metric labels are not a []string literal")
	}
	labels := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		label, ok := src.str(elt)
		if !ok {
			return nil, src.errorf(elt, "metric label is not a constant string")
		}
		labels = append(labels, label)
	}

	return labels, nil
}
