package main_test

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	docsgen "github.com/NethermindEth/starknet-staking-v2/tools/docsgen"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	commandFile = "cmd/validator/main.go"
	configFile  = "validator/config/validator_config.go"
	metricsFile = "validator/metrics/metrics.go"
)

func fixture(t *testing.T) fstest.MapFS {
	t.Helper()

	root := filepath.Join("testdata", "src")
	files := fstest.MapFS{}
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		files[path] = &fstest.MapFile{Data: data}

		return err
	})
	require.NoError(t, err)

	return files
}

func TestGenerate(t *testing.T) {
	files, err := docsgen.Generate(fixture(t))
	require.NoError(t, err)
	require.Len(t, files, 2)

	for _, name := range []string{docsgen.OptionsFile, docsgen.MetricsFile} {
		golden := filepath.Join("testdata", "golden", name)
		if *update {
			require.NoError(t, os.WriteFile(golden, files[name], 0o600))
		}
		want, err := os.ReadFile(golden)
		require.NoError(t, err)
		require.Equal(t, string(want), string(files[name]), name)
	}
}

func TestGenerateRepository(t *testing.T) {
	files, err := docsgen.Generate(os.DirFS(filepath.Join("..", "..")))
	require.NoError(t, err)
	require.Contains(t, string(files[docsgen.OptionsFile]), "| `--max-retries` |")
	require.Contains(t, string(files[docsgen.MetricsFile]), "| `validator_attestation_")
}

func TestGenerateFailsOnUnknownShapes(t *testing.T) {
	tests := []struct {
		name string
		file string
		old  string
		new  string
		want string
	}{
		{
			name: "flag registered without a target",
			file: commandFile,
			old:  `cmd.Flags().BoolVar(&metricsF, "metrics", false,`,
			new:  `cmd.Flags().Bool("metrics", false,`,
			want: "unsupported Flags().Bool call",
		},
		{
			name: "persistent flag",
			file: commandFile,
			old:  `cmd.Flags().BoolVar(&metricsF,`,
			new:  `cmd.PersistentFlags().BoolVar(&metricsF,`,
			want: "unsupported PersistentFlags().BoolVar call",
		},
		{
			name: "usage built at runtime",
			file: commandFile,
			old:  `"Provider http address"`,
			new:  `fmt.Sprintf("Provider %s address", "http")`,
			want: "--provider-http usage is not a constant string",
		},
		{
			name: "default that needs an override",
			file: commandFile,
			old:  "100.5,",
			new:  "defaultThreshold(),",
			want: "--balance-threshold default defaultThreshold() cannot be evaluated",
		},
		{
			name: "registered default no longer matches its override",
			file: commandFile,
			old:  "log.INFO.String()",
			new:  "log.DEBUG.String()",
			want: "--log-level default log.DEBUG.String() does not match its override",
		},
		{
			name: "duplicate flag",
			file: commandFile,
			old:  `"metrics", false`,
			new:  `"config", false`,
			want: "--config is registered twice",
		},
		{
			name: "flag bound to an unknown config field",
			file: commandFile,
			old:  "&config.Provider.HTTP",
			new:  "&config.Provider.Host",
			want: "Provider has no field Host",
		},
		{
			name: "config field without a json tag",
			file: configFile,
			old:  "`json:\"claimMaxFee,omitempty\"`",
			new:  "`yaml:\"claimMaxFee\"`",
			want: "Reward.ClaimMaxFee has no json tag",
		},
		{
			name: "config field without an env var",
			file: configFile,
			old:  `Provider{HTTP: os.Getenv("PROVIDER_HTTP_URL")}`,
			new:  `Provider{HTTP: ""}`,
			want: "no os.Getenv call fills Provider.HTTP",
		},
		{
			name: "unsupported metric type",
			file: metricsFile,
			old:  "prometheus.NewCounterVec(",
			new:  "prometheus.NewHistogramVec(",
			want: "unsupported metric constructor NewHistogramVec",
		},
		{
			name: "help built at runtime",
			file: metricsFile,
			old:  `"The latest block number seen by the validator"`,
			new:  "help()",
			want: "metric Help is not a constant string",
		},
		{
			name: "metric option the table cannot show",
			file: metricsFile,
			old:  `Name: "validator_attestation_uptime_seconds",`,
			new:  `Name: "uptime_seconds", Namespace: "validator",`,
			want: "unsupported metric option Namespace",
		},
		{
			name: "metric not registered",
			file: metricsFile,
			old:  "registry.MustRegister(m.uptime)",
			new:  "",
			want: "3 metrics declared but 2 passed to MustRegister",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := fixture(t)
			source := string(files[test.file].Data)
			require.Contains(t, source, test.old)
			files[test.file].Data = []byte(strings.Replace(source, test.old, test.new, 1))

			_, err := docsgen.Generate(files)
			require.ErrorContains(t, err, test.want)
			require.ErrorContains(t, err, test.file+":")
		})
	}
}

func TestGenerateFailsOnMissingFile(t *testing.T) {
	for _, file := range []string{commandFile, configFile, metricsFile} {
		files := fixture(t)
		delete(files, file)

		_, err := docsgen.Generate(files)
		require.ErrorContains(t, err, file)
	}
}
