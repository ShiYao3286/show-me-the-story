//go:build windows

// Package tray shows a notification-area icon for the running server.
//
// The project keeps a zero-dependency stance, so this talks to Win32 through
// plain syscall instead of a tray library: RegisterClassExW + CreateWindowExW +
// Shell_NotifyIconW + a message loop. Icons are simple enough that pulling in a
// third-party module (and losing the one-command cross-compile) is not worth it.
package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Callbacks are the actions the tray menu can trigger. All of them are invoked
// on the message-loop thread; implementations must not block it for long.
type Callbacks struct {
	// OpenUI is called for a left-click on the icon as well as the menu item.
	OpenUI func()
	// OpenDataDir opens the program directory in Explorer.
	OpenDataDir func()
	// Quit is called when the user picks "exit". The tray has already removed
	// its icon by then, so Quit may shut the server down and return.
	Quit func()
}

// Options configures the icon and menu.
type Options struct {
	Tooltip string
	// IconICO is the .ico shown in the notification area. A multi-size ICO lets
	// Windows pick the size it needs (16px in the tray, 32px on high DPI).
	IconICO []byte
	// Labels are the menu item texts.
	OpenUILabel   string
	OpenDataLabel string
	QuitLabel     string
	// AboutItem, when non-empty, is added above the separator as a disabled line.
	AboutItem string
}

// Errors returned by Run.
var (
	ErrAlreadyRunning = errors.New("tray already running")
	ErrInitFailed     = errors.New("tray initialization failed")
)

var (
	runningOnce sync.Once
	runErr      error
)

// Run creates the tray icon and blocks the calling goroutine in the Win32
// message loop until the user quits or the process exits. It must be called on
// the main goroutine: the window it creates is owned by that OS thread, so the
// goroutine is locked to it for the lifetime of the call.
//
// Run returns nil after a normal quit.
func Run(opts Options, cb Callbacks) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	runningOnce.Do(func() {
		runErr = runLocked(opts, cb)
	})
	return runErr
}

// IsSupported reports whether a tray can be shown on this platform.
func IsSupported() bool { return true }

// win32 bindings ------------------------------------------------------------

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	procAppendMenuW       = user32.NewProc("AppendMenuW")
	procDestroyMenu       = user32.NewProc("DestroyMenu")
	procTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	procGetCursorPos      = user32.NewProc("GetCursorPos")
	procSetForegroundWnd  = user32.NewProc("SetForegroundWindow")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procLoadImageW        = user32.NewProc("LoadImageW")
	procDestroyIcon       = user32.NewProc("DestroyIcon")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW  = shell32.NewProc("Shell_NotifyIconW")
)

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmContextMenu = 0x007B
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmApp         = 0x8000 // WM_APP: tray callback messages

	// Tray callback message ids (WM_APP + n).
	wmTrayIcon = wmApp + 1

	csVRedraw = 0x0001
	csHRedraw = 0x0002

	wsOverlappedWindow = 0x00CF0000
	wsExToolWindow     = 0x00000080

	niFMessage  = 0x00000001
	niFIcon     = 0x00000002
	niFTip      = 0x00000004
	niFShowTip  = 0x00000080
	nimAdd      = 0x00000000
	nimModify   = 0x00000001
	nimDelete   = 0x00000002

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfGrayed    = 0x00000001

	tpmLeftAlign   = 0x0000
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
)

type point struct{ X, Y int32 }

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type msgT struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type notifyIconDataW struct {
	cbSize           uint32
	hWnd             syscall.Handle
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            syscall.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     syscall.Handle
}

// Command ids used in the popup menu.
const (
	cmdOpenUI   = 1001
	cmdOpenData = 1002
	cmdQuit     = 1003
)

// Menu/icon state kept for the window-proc callback. The window proc cannot
// capture Go closures, so these live at package scope; only one tray exists per
// process, which matches how the app runs.
var (
	gOpts   Options
	gCB     Callbacks
	gNID    notifyIconDataW
	gMenu   syscall.Handle
	gIcon   syscall.Handle
	gHwnd   syscall.Handle
	gQuitConfirmed bool
	gClass  = "ShowMeTheStoryTrayWnd"
)

func runLocked(opts Options, cb Callbacks) error {
	gOpts, gCB = opts, cb

	hInstance, _, _ := procGetModuleHandleW.Call(0)

	className, err := syscall.UTF16PtrFromString(gClass)
	if err != nil {
		return err
	}

	// 1. Register a window class. The window is never shown; it only exists to
	//    receive the tray callback messages.
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     syscall.Handle(hInstance),
		lpszClassName: className,
	}
	if atom, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return fmt.Errorf("%w: RegisterClassExW: %v", ErrInitFailed, e)
	}

	// 2. Create the (hidden) message window.
	hwnd, _, e := procCreateWindowExW.Call(
		wsExToolWindow,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(className)),
		wsOverlappedWindow,
		0, 0, 0, 0,
		0, 0,
		hInstance,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("%w: CreateWindowExW: %v", ErrInitFailed, e)
	}
	gHwnd = syscall.Handle(hwnd)

	// 3. Load the icon from the embedded PNG bytes.
	icon, err := loadIconFromICO(opts.IconICO)
	if err != nil {
		procDestroyWindow.Call(hwnd)
		return err
	}
	gIcon = icon

	// 4. Add the notification-area icon.
	gNID = notifyIconDataW{
		cbSize:           uint32(unsafe.Sizeof(notifyIconDataW{})),
		hWnd:             gHwnd,
		uID:              1,
		uFlags:           niFMessage | niFIcon | niFTip | niFShowTip,
		uCallbackMessage: wmTrayIcon,
		hIcon:            gIcon,
	}
	copy(gNID.szTip[:], utf16FromString(truncateUTF16(opts.Tooltip, 127)))
	if r, _, e := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&gNID))); r == 0 {
		procDestroyIcon.Call(uintptr(gIcon))
		procDestroyWindow.Call(hwnd)
		return fmt.Errorf("%w: Shell_NotifyIcon: %v", ErrInitFailed, e)
	}

	// 5. Build the context menu once; it is shown on demand.
	gMenu = buildMenu()

	// 6. Pump messages until WM_QUIT.
	var m msgT
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			return fmt.Errorf("%w: GetMessage failed", ErrInitFailed)
		}
		if r == 0 {
			// WM_QUIT
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	cleanup()
	return nil
}

// cleanup removes the icon and destroys the Win32 objects. Safe to call twice.
func cleanup() {
	if gMenu != 0 {
		procDestroyMenu.Call(uintptr(gMenu))
		gMenu = 0
	}
	if gHwnd != 0 {
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&gNID)))
		procDestroyWindow.Call(uintptr(gHwnd))
		gHwnd = 0
	}
	if gIcon != 0 {
		procDestroyIcon.Call(uintptr(gIcon))
		gIcon = 0
	}
}

func buildMenu() syscall.Handle {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return 0
	}
	appendMenu(menu, mfString, cmdOpenUI, gOpts.OpenUILabel)
	appendMenu(menu, mfString, cmdOpenData, gOpts.OpenDataLabel)
	if gOpts.AboutItem != "" {
		appendMenu(menu, mfString|mfGrayed, 0, gOpts.AboutItem)
	}
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdQuit, gOpts.QuitLabel)
	return syscall.Handle(menu)
}

func appendMenu(menu uintptr, flags uintptr, id uintptr, label string) {
	if label == "" {
		procAppendMenuW.Call(menu, flags, id, 0)
		return
	}
	p, err := syscall.UTF16PtrFromString(label)
	if err != nil {
		return
	}
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
}

// wndProc handles the messages posted to the hidden window.
func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayIcon:
		// lParam's low word is the mouse message.
		switch uint32(lParam) & 0xFFFF {
		case wmLButtonUp, wmLButtonDbl:
			if gCB.OpenUI != nil {
				gCB.OpenUI()
			}
		case wmRButtonUp:
			showContextMenu(hwnd)
		}
		return 0
	case wmClose:
		// Route every quit path through the callback so the host can shut the
		// server down gracefully. If the callback declines (e.g. the user
		// cancelled because a task is running), the window stays alive.
		if gCB.Quit != nil {
			gCB.Quit()
		}
		if gQuitConfirmed {
			procDestroyWindow.Call(uintptr(hwnd))
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func showContextMenu(hwnd syscall.Handle) {
	if gMenu == 0 {
		return
	}
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// Required by the shell: without SetForegroundWindow the menu will not
	// dismiss when the user clicks elsewhere.
	procSetForegroundWnd.Call(uintptr(hwnd))

	ret, _, _ := procTrackPopupMenu.Call(
		uintptr(gMenu),
		tpmLeftAlign|tpmRightButton|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y),
		0, uintptr(hwnd), 0,
	)

	// Documented workaround so the menu closes properly.
	procPostMessageW.Call(uintptr(hwnd), wmClose+1 /* WM_NULL */, 0, 0)

	switch ret {
	case cmdOpenUI:
		if gCB.OpenUI != nil {
			gCB.OpenUI()
		}
	case cmdOpenData:
		if gCB.OpenDataDir != nil {
			gCB.OpenDataDir()
		}
	case cmdQuit:
		// Let the host run its own shutdown (it may ask for confirmation when a
		// task is in flight). The icon is removed by cleanup() on the way out.
		if gCB.Quit != nil {
			gCB.Quit()
		}
		if gQuitConfirmed {
			procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&gNID)))
			procPostQuitMessage.Call(0)
		}
	}
}

// ConfirmQuit tells the message loop that the quit may proceed. Callers invoke
// it from the Quit callback once any confirmation dialog has been accepted and
// the server has finished shutting down.
func ConfirmQuit() {
	gQuitConfirmed = true
}

// Quit asks the message loop to exit from another goroutine.
func Quit() {
	if gHwnd != 0 {
		procPostMessageW.Call(uintptr(gHwnd), wmClose, 0, 0)
	}
}

// loadIconFromICO turns the embedded .ico bytes into an HICON.
//
// Win32 has no in-memory image decoder reachable through plain syscall, but
// LoadImageW reads an .ico from disk in every supported Windows version. The
// file is written to %TEMP% and removed immediately after loading.
func loadIconFromICO(ico []byte) (syscall.Handle, error) {
	if len(ico) == 0 {
		return 0, fmt.Errorf("%w: no icon data embedded", ErrInitFailed)
	}
	return loadIconViaTempFile(ico, true)
}

func utf16FromString(s string) []uint16 {
	out, err := syscall.UTF16FromString(s)
	if err != nil {
		return []uint16{0}
	}
	return out
}

func truncateUTF16(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}
