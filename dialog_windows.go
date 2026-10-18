//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	trayUser32 = syscall.NewLazyDLL("user32.dll")

	procMessageBoxW = trayUser32.NewProc("MessageBoxW")
)

const (
	mbOKCancel        = 0x00000001
	mbIconWarning     = 0x00000030
	mbDefaultSecond   = 0x00000100
	idOK              = 1
)

// confirmQuit shows a modal yes/no dialog and reports whether the user chose OK.
// Runs on the message-loop thread so the dialog is owned by the tray window.
func confirmQuit(text string) bool {
	title := "AI 小说写手"
	body, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return false
	}
	head, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	r, _, _ := procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(body)),
		uintptr(unsafe.Pointer(head)),
		mbOKCancel|mbIconWarning|mbDefaultSecond,
	)
	return r == idOK
}
