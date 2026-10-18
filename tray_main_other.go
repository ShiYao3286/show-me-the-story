//go:build !windows

package main

import (
	"io/fs"

	"showmethestory/internal/config"
	"showmethestory/internal/sse"
)

// runWithTray is a no-op off Windows: those platforms use the plain blocking
// server, which matches the documented "run it in a terminal" usage.
func runWithTray(apiCfg *config.APIConfig, apiCfgPath string, logger *sse.LogBroadcaster, port, progDir, version string, staticFS fs.FS) bool {
	return false
}
