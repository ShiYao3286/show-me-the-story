//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"showmethestory/internal/tray"
)

// startTray runs the app with a notification-area icon. It returns after the
// user quits from the tray menu, having shut the HTTP server down gracefully.
//
// The Win32 message loop must own the main goroutine (the window it creates
// belongs to that OS thread), so the server is started in the background here
// and the blocking StartWebServer path is not used.
func startTray(runServer func(openBrowser bool) (*serverHandle, error)) error {
	srv, err := runServer(false)
	if err != nil {
		return err
	}

	// Give the listener a moment to be reachable before the first click.
	url := "http://" + srv.addr()

	opts := tray.Options{
		Tooltip:       fmt.Sprintf("AI 小说写手 %s\n%s", version, url),
		IconICO:       trayIconICO,
		OpenUILabel:   "打开写作界面",
		OpenDataLabel: "打开数据目录",
		QuitLabel:     "退出",
		AboutItem:     fmt.Sprintf("版本 %s · %s", version, url),
	}

	cb := tray.Callbacks{
		OpenUI: func() {
			openInBrowser(url)
		},
		OpenDataDir: func() {
			openFolder(srv.progDirPath())
		},
		Quit: func() {
			// Ask before dropping a running task: a hard exit can interrupt a
			// save. The confirmation runs on the message-loop thread, which is
			// what Win32 expects for a modal dialog.
			if srv.taskRunning() {
				if !confirmQuit("有 AI 任务正在运行。\n\n现在退出可能中断正在保存的章节（下次打开时可能需要恢复）。\n\n仍要退出吗？") {
					// Declined: leave the tray and server running.
					return
				}
			}
			shutdownGracefully(srv)
			tray.ConfirmQuit()
		},
	}

	// Open the browser once at startup, matching the non-tray behaviour.
	openInBrowser(url)

	return tray.Run(opts, cb)
}

// shutdownGracefully stops accepting connections and lets in-flight requests
// finish before the process exits.
func shutdownGracefully(srv *serverHandle) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.shutdown(ctx); err != nil {
		fmt.Printf(" [系统] 关闭服务时出错: %v\n", err)
	} else {
		fmt.Println(" [系统] 服务已关闭")
	}
}

// openInBrowser opens the UI in the default browser.
func openInBrowser(url string) {
	// Slight delay so the first launch does not race the listener.
	time.Sleep(200 * time.Millisecond)
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// openFolder opens a directory in Explorer.
func openFolder(dir string) {
	_ = exec.Command("explorer", dir).Start()
}
