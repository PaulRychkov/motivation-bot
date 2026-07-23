package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Мотиватор",
		Width:            480,
		Height:           760,
		MinWidth:         360,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 28, G: 30, B: 38, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.Startup,
		Bind:             []any{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
