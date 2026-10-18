package main

import (
	"context"
	"io/fs"

	"showmethestory/internal/config"
	"showmethestory/internal/httpapi"
	"showmethestory/internal/sse"
)

// serverHandle is the platform-agnostic wrapper around the running HTTP server
// that the tray (and, on other platforms, the plain blocking path) drives.
type serverHandle struct {
	ws      *httpapi.WebServer
	progDir string
}

// startServerNonBlocking binds the port and starts serving in the background.
func startServerNonBlocking(apiCfg *config.APIConfig, apiCfgPath string, logger *sse.LogBroadcaster, port, progDir, version string, staticFiles fs.FS, openOnStart bool) (*serverHandle, error) {
	ws, err := httpapi.StartWebServerNonBlocking(apiCfg, apiCfgPath, logger, port, progDir, version, staticFiles, openOnStart)
	if err != nil {
		return nil, err
	}
	return &serverHandle{ws: ws, progDir: progDir}, nil
}

func (s *serverHandle) addr() string { return s.ws.Addr() }

func (s *serverHandle) progDirPath() string { return s.progDir }

func (s *serverHandle) taskRunning() bool { return s.ws.TaskRunning() }

func (s *serverHandle) shutdown(ctx context.Context) error { return s.ws.Shutdown(ctx) }
