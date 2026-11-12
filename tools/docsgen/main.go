package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	OptionsFile = "_config-options.md"
	MetricsFile = "_metrics.md"

	commandPath = "cmd/validator/main.go"
	configPath  = "validator/config/validator_config.go"
	metricsPath = "validator/metrics/metrics.go"

	partialPerm = 0o600
)

func Generate(src fs.FS) (map[string][]byte, error) {
	command, err := parseSource(src, commandPath)
	if err != nil {
		return nil, err
	}
	configSrc, err := parseSource(src, configPath)
	if err != nil {
		return nil, err
	}
	metricsSrc, err := parseSource(src, metricsPath)
	if err != nil {
		return nil, err
	}

	config, err := newConfigModel(configSrc)
	if err != nil {
		return nil, err
	}
	options, err := parseOptions(command, config)
	if err != nil {
		return nil, err
	}
	metrics, err := parseMetrics(metricsSrc)
	if err != nil {
		return nil, err
	}

	return map[string][]byte{
		OptionsFile: renderOptions(options),
		MetricsFile: renderMetrics(metrics),
	}, nil
}

func run(src, out string) error {
	files, err := Generate(os.DirFS(src))
	if err != nil {
		return err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(out, name), data, partialPerm); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	src := flag.String("src", ".", "root of the validator source tree to document")
	out := flag.String("out", "docs/docs", "directory to write the generated partials to")
	flag.Parse()

	if err := run(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "docsgen:", err)
		os.Exit(1)
	}
}
