//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/getlantern/systray"

	"kills/internal/config"
	"kills/internal/server"
	"kills/internal/winutil"
)

//go:embed assets/icon.ico
var iconData []byte

func main() {
	cfgPath, err := config.ConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	store, err := config.NewStore(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	app := &application{store: store}
	if err := app.run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type application struct {
	store      *config.Store
	httpServer *http.Server
	mu         sync.Mutex
	shutdown   context.CancelFunc
}

func (a *application) run() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.shutdown = cancel

	if err := a.startHTTPServer(); err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		systray.Run(a.onReady, a.onExit)
		close(done)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigCh:
		cancel()
		systray.Quit()
	case <-ctx.Done():
		systray.Quit()
	case <-done:
	}

	a.stopHTTPServer()
	return nil
}

func (a *application) baseURL() string {
	port := a.store.Get().Port
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (a *application) startHTTPServer() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg := a.store.Get()
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	srv := server.New(a.store, a.baseURL)
	a.httpServer = &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}

	go func() {
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "http:", err)
		}
	}()

	// brief wait for listener
	time.Sleep(80 * time.Millisecond)
	return nil
}

func (a *application) stopHTTPServer() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.httpServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = a.httpServer.Shutdown(ctx)
	a.httpServer = nil
}

func (a *application) onReady() {
	if len(iconData) > 0 {
		systray.SetIcon(iconData)
	}
	systray.SetTitle("进程终结器")
	systray.SetTooltip("进程终结器 - 点击打开配置页")

	mOpen := systray.AddMenuItem("打开页面", "在浏览器中打开")
	mRestart := systray.AddMenuItem("重启", "重启本程序")
	mQuit := systray.AddMenuItem("退出", "退出程序")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser(a.baseURL())
			case <-mRestart.ClickedCh:
				restartSelf()
			case <-mQuit.ClickedCh:
				systray.Quit()
				if a.shutdown != nil {
					a.shutdown()
				}
				return
			}
		}
	}()
}

func (a *application) onExit() {
	a.stopHTTPServer()
}

func openBrowser(url string) {
	_ = winutil.OpenURL(url)
}

func restartSelf() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, _ = filepath.Abs(exe)
	cmd := exec.Command(exe)
	winutil.HideConsole(cmd)
	_ = cmd.Start()
	systray.Quit()
	os.Exit(0)
}
