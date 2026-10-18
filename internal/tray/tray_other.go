//go:build !windows

// Package tray is a no-op on platforms without a system tray.
//
// The app's supported non-Windows usage is "run it in a terminal", so there is
// nothing to integrate with here; Run returns ErrUnsupported so callers can fall
// back to the plain blocking server.
package tray

import "errors"

// ErrUnsupported is returned by Run on platforms without tray support.
var ErrUnsupported = errors.New("system tray is only available on Windows")

// Options mirrors the Windows API so callers can be platform-agnostic.
type Options struct {
	Tooltip       string
	IconICO       []byte
	OpenUILabel   string
	OpenDataLabel string
	QuitLabel     string
	AboutItem     string
}

// Callbacks mirrors the Windows API.
type Callbacks struct {
	OpenUI      func()
	OpenDataDir func()
	Quit        func()
}

// IsSupported reports whether a tray can be shown on this platform.
func IsSupported() bool { return false }

// Run always fails; the caller should use the blocking server instead.
func Run(opts Options, cb Callbacks) error { return ErrUnsupported }

// Quit is a no-op off Windows.
func Quit() {}

// ConfirmQuit is a no-op off Windows.
func ConfirmQuit() {}
