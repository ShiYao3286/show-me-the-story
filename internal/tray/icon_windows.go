//go:build windows

package tray

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// loadIconViaTempFile decodes an embedded image into an HICON.
//
// The input may be a PNG or an .ico. PNG goes through LoadImageW with an
// explicit size (a PNG with cx/cy == 0 makes LoadImageW fail); an .ico is read
// with LR_DEFAULTCOLOR which picks the best size itself.
func loadIconViaTempFile(img []byte, wantICO bool) (syscall.Handle, error) {
	pattern := "smts-tray-*.png"
	if wantICO {
		pattern = "smts-tray-*.ico"
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return 0, fmt.Errorf("%w: 创建图标临时文件失败: %v", ErrInitFailed, err)
	}
	name := f.Name()
	defer os.Remove(name)

	if _, err := f.Write(img); err != nil {
		f.Close()
		return 0, fmt.Errorf("%w: 写入图标失败: %v", ErrInitFailed, err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("%w: 关闭图标文件失败: %v", ErrInitFailed, err)
	}

	abs, err := filepath.Abs(name)
	if err != nil {
		return 0, err
	}
	pathPtr, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return 0, err
	}

	const (
		imageIcon      = 1
		lrLoadFromFile = 0x00000010
		lrDefaultSize  = 0x00000040
	)

	cx, cy := uintptr(0), uintptr(0)
	if !wantICO {
		// LoadImageW requires an explicit size for PNG input.
		cx, cy = 32, 32
	}

	h, _, e := procLoadImageW.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		imageIcon,
		cx, cy,
		lrLoadFromFile|lrDefaultSize,
	)
	if h == 0 {
		return 0, fmt.Errorf("%w: LoadImageW: %v", ErrInitFailed, e)
	}
	return syscall.Handle(h), nil
}
