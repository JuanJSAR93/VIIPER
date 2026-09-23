//go:build !windows

package api

func autoAttachHost() string { return "localhost" }
