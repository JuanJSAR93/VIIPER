package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Alia5/VIIPER/internal/config"
	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestLegacyUpdateNotifyFlagsAndEnvironmentCannotEnableSelfUpdates(t *testing.T) {
	for _, mode := range []string{"none", "stable", "prerelease", "legacy-custom-value"} {
		for _, fromEnvironment := range []bool{false, true} {
			name := mode + "/flag"
			if fromEnvironment {
				name = mode + "/environment"
			}
			t.Run(name, func(t *testing.T) {
				unsetUpdateTestEnvironment(t, "VIIPER_UPDATE_NOTIFY")
				args := []string{"--update-notify=" + mode, "proxy"}
				if fromEnvironment {
					t.Setenv("VIIPER_UPDATE_NOTIFY", mode)
					args = []string{"proxy"}
				}
				var cli config.CLI
				p, err := kong.New(&cli)
				require.NoError(t, err)
				ctx, err := p.Parse(append(args, "--upstream-addr=127.0.0.1:3241"))
				require.NoError(t, err, "Previously accepted settings must remain parseable")
				require.Equal(t, "proxy", ctx.Command())
				require.Equal(t, config.UpdateNotifyNone, cli.UpdateNotify)
				// Only parse. Never run proxy/server, startup registration or HID.
			})
		}
	}
}

func TestSelfUpdatesDefaultToDisabled(t *testing.T) {
	unsetUpdateTestEnvironment(t, "VIIPER_UPDATE_NOTIFY")
	var cli config.CLI
	p, err := kong.New(&cli)
	require.NoError(t, err)
	_, err = p.Parse([]string{"proxy", "--upstream-addr=127.0.0.1:3241"})
	require.NoError(t, err)
	require.Equal(t, config.UpdateNotifyNone, cli.UpdateNotify)
}

func TestLegacyUpdateNotifyConfigurationRemainsCompatibleAndInert(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		for _, format := range []string{"json", "yaml", "toml"} {
			t.Run(format+"/exclusive="+strconv.FormatBool(exclusive), func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				t.Setenv("AppData", root)
				t.Setenv("XDG_CONFIG_HOME", root)
				unsetUpdateTestEnvironment(t, "VIIPER_CONFIG")
				unsetUpdateTestEnvironment(t, "VIIPER_UPDATE_NOTIFY")
				unsetUpdateTestEnvironment(t, "VIIPER_LOG_LEVEL")
				data := map[string]string{
					"json": `{"update_notify":"prerelease","log.level":"warn"}`,
					"yaml": "update-notify: prerelease\nlog.level: warn\n",
					"toml": "update-notify = \"prerelease\"\n\"log.level\" = \"warn\"\n",
				}[format]
				path := filepath.Join(root, "selected."+format)
				require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
				args := []string{"--config", path}
				if exclusive {
					args = append(args, "--config-only")
				}
				options, only, err := configurationOptions(args)
				require.NoError(t, err)
				require.Equal(t, exclusive, only)
				var cli config.CLI
				p, err := kong.New(&cli, options...)
				require.NoError(t, err)
				_, err = p.Parse(append(args, "proxy", "--upstream-addr=127.0.0.1:3241"))
				require.NoError(t, err)
				require.Equal(t, config.UpdateNotifyNone, cli.UpdateNotify)
				require.Equal(t, "warn", cli.Log.Level, "Removing self-update must not discard other saved settings")
				_, err = p.Parse(append(args, "--update-notify=stable", "--log.level=debug", "proxy", "--upstream-addr=127.0.0.1:3241"))
				require.NoError(t, err)
				require.Equal(t, config.UpdateNotifyNone, cli.UpdateNotify)
				require.Equal(t, "debug", cli.Log.Level)
			})
		}
	}
}

func unsetUpdateTestEnvironment(t *testing.T, name string) {
	t.Helper()
	// Setenv registers restoration of the original environment. An unset value,
	// unlike an empty value, does not override Kong defaults/configuration files.
	t.Setenv(name, "")
	require.NoError(t, os.Unsetenv(name))
}

func TestRuntimeSelfUpdaterAndStartupHookAreRemoved(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	files, err := filepath.Glob(filepath.Join(root, "internal", "updater", "*.go"))
	require.NoError(t, err)
	require.Empty(t, files, "Remove the network, dialog, dismissal-file and installer-execution implementation, not just its default")

	for _, directory := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			for _, imported := range file.Imports {
				name, err := strconv.Unquote(imported.Path.Value)
				require.NoError(t, err)
				require.NotContains(t, name, "/internal/updater", path)
			}
			if filepath.Base(path) == "viiper.go" {
				ast.Inspect(file, func(node ast.Node) bool {
					if selector, ok := node.(*ast.SelectorExpr); ok {
						require.NotEqual(t, "UpdateNotify", selector.Sel.Name,
							"Startup must not consult the ignored legacy option to create an update worker")
					}
					return true
				})
			}
			return nil
		})
		require.NoError(t, err)
	}
}
