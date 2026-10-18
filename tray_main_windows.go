//go:build windows

package main

import (
	"io/fs"
	"os"
	"strings"

	"showmethestory/internal/config"
	"showmethestory/internal/sse"
)

// runWithTray starts the server plus a notification-area icon and blocks until
// the user quits. Returns true when it handled startup (so main must not fall
// back to the blocking server).
//
// Setting SMTS_NO_TRAY=1 disables the icon, which is useful for running the app
// from a terminal or under a debugger.
func runWithTray(apiCfg *config.APIConfig, apiCfgPath string, logger *sse.LogBroadcaster, port, progDir, version string, staticFS fs.FS) bool {
	if strings.TrimSpace(os.Getenv("SMTS_NO_TRAY")) != "" {
		fmtTrayDisabled()
		return false
	}

	err := startTray(func(openBrowser bool) (*serverHandle, error) {
		return startServerNonBlocking(apiCfg, apiCfgPath, logger, port, progDir, version, staticFS, openBrowser)
	})
	if err != nil {
		// Fall back to the plain server so a tray failure never blocks use.
		printTrayFailed(err)
		return false
	}
	return true
}

func fmtTrayDisabled() {
	println(" [系统] SMTS_NO_TRAY 已设置，跳过系统托盘")
}

func printTrayFailed(err error) {
	println(" [系统] 系统托盘启动失败，改用普通模式:", err.Error())
}
