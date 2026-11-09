// RUNTIDE desktop application entry point (Wails).
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/runtide/runtide/internal/desktop"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a, err := desktop.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "RUNTIDE failed to start:", err)
		os.Exit(1)
	}
	for _, arg := range os.Args[1:] {
		if arg == "--background" {
			a.Hidden = true
		}
	}
	err = wails.Run(&options.App{
		Title:            "RUNTIDE",
		Width:            1280,
		Height:           800,
		MinWidth:         980,
		MinHeight:        620,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 243, G: 243, B: 243, A: 255},
		OnStartup:        a.Startup,
		OnBeforeClose:    a.BeforeClose,
		OnShutdown:       a.Shutdown,
		Bind:             []any{a},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "6f2c1d2a-5b1e-4c58-9d0a-runtide-desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { a.Show() },
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.SystemDefault,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "RUNTIDE error:", err)
		os.Exit(1)
	}
}
