// Package main wires the FyneTune desktop application.
package main

import (
	_ "embed"
	"log/slog"
	"net/http"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"go.lucor.dev/fynetune/internal/audio/otooutput"
	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
	"go.lucor.dev/fynetune/internal/transport/httpstream"
	"go.lucor.dev/fynetune/internal/ui"
	"go.lucor.dev/fynetune/internal/version"
)

const (
	appID                   = "dev.lucor.fynetune"
	appName                 = "FyneTune"
	appIconName             = "icon.svg"
	radioBrowserHTTPTimeout = 15 * time.Second
	streamHeaderTimeout     = 15 * time.Second
)

//go:embed internal/ui/assets/icon.svg
var appIconSVG []byte

func main() {
	configureLogging()
	a := app.NewWithID(appID)
	metadata := a.Metadata()
	metadata.ID = appID
	metadata.Name = appName
	metadata.Icon = fyne.NewStaticResource(appIconName, appIconSVG)
	if metadata.Build < 1 {
		metadata.Build = 1
	}
	version.SetApplicationMetadata(metadata)
	if !metadata.Release {
		metadata.Version = version.Current().Version
	}
	app.SetMetadata(metadata)
	radioBrowserHTTPClient := &http.Client{Timeout: radioBrowserHTTPTimeout}
	radioBrowserClient := radiobrowser.New(radioBrowserHTTPClient)
	streamHTTPClient := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: streamHeaderTimeout}}
	radioPlayer := radio.NewPlayer(httpstream.New(streamHTTPClient), codec.New(), otooutput.New())
	window := ui.New(a, radioPlayer, radioBrowserClient)
	window.Run()
}

// configureLogging sets the minimum slog level. Debug diagnostics can be
// enabled for a run with FYNETUNE_LOG_LEVEL=debug.
func configureLogging() {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	if value := os.Getenv("FYNETUNE_LOG_LEVEL"); value != "" {
		if err := level.UnmarshalText([]byte(value)); err != nil {
			slog.Warn("invalid FYNETUNE_LOG_LEVEL; using info", "value", value, "error", err)
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}
