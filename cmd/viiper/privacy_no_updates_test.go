package main

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Alia5/VIIPER/internal/config"
	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestUpdateNotificationFlagIsNotPartOfTheCLI(t *testing.T) {
	var cli config.CLI
	p, err := kong.New(&cli)
	require.NoError(t, err)
	_, err = p.Parse([]string{
		"--update-notify=stable",
		"proxy",
		"--upstream-addr=127.0.0.1:3241",
	})
	require.Error(t, err)
}

func TestNoRuntimeUpdaterPackageExists(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	files, err := filepath.Glob(filepath.Join(root, "internal", "updater", "*.go"))
	require.NoError(t, err)
	require.Empty(t, files)
}
