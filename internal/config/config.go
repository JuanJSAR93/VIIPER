// Package config defines the CLI structure and configuration for VIIPER.
package config

import (
	"github.com/Alia5/VIIPER/internal/cmd"
)

// UpdateNotify is retained only to parse legacy flags and saved configuration.
// VIIPER no longer contains a built-in update checker or installer.
type UpdateNotify string

const (
	UpdateNotifyNone       UpdateNotify = "none"
	UpdateNotifyStable     UpdateNotify = "stable"
	UpdateNotifyPrerelease UpdateNotify = "prerelease"
)

type Log struct {
	Level   string `aliases:"l" help:"Log level: trace, debug, info, warn, error" default:"info" env:"VIIPER_LOG_LEVEL"`
	File    string `help:"Log file path (default: none; logs only to console)" env:"VIIPER_LOG_FILE"`
	RawFile string `help:"Raw packet log file path (default: none)" env:"VIIPER_LOG_RAW_FILE"`
}

// CLI is the root command structure for Kong CLI parsing.
type CLI struct {
	// Global
	ConfigPath   string       `help:"Path to configuration file (json|yaml|toml)" name:"config" env:"VIIPER_CONFIG"`
	ConfigOnly   bool         `help:"Load only the explicit absolute --config file; never search default locations"`
	UpdateNotify UpdateNotify `help:"Deprecated and ignored; built-in self-updates have been removed" default:"none" env:"VIIPER_UPDATE_NOTIFY"`
	Log          `embed:"" prefix:"log."`
	codegenCommand

	Server cmd.Server `cmd:"" help:"Start the VIIPER USB-IP server" default:""`
	Proxy  cmd.Proxy  `cmd:"" help:"Start the VIIPER USB-IP proxy"`

	Config    cmd.ConfigCommand `cmd:"" help:"Manage configuration files"`
	Install   cmd.Install       `cmd:"" help:"Add the current VIIPER executable to system startup and runs it (creates a Systemd service on Linux)"`
	Uninstall cmd.Uninstall     `cmd:"" help:"Remove any VIIPER system startup configuration / Systemd service"`
}

// AfterApply makes legacy values inert without rejecting previously valid
// command lines, environment variables, or configuration files.
func (c *CLI) AfterApply() error {
	c.UpdateNotify = UpdateNotifyNone
	return nil
}
