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
	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
	"go.lucor.dev/fynetune/internal/storage"
	"go.lucor.dev/fynetune/internal/transport/httpstream"
	"go.lucor.dev/fynetune/internal/ui"
	"go.lucor.dev/fynetune/internal/version"
)

const (
	appID                   = "dev.lucor.fynetune"
	debugAppID              = "dev.lucor.fynetune-debug"
	appName                 = "FyneTune"
	appIconName             = "icon.svg"
	radioBrowserHTTPTimeout = 15 * time.Second
	streamHeaderTimeout     = 15 * time.Second
)

//go:embed internal/ui/assets/icon.svg
var appIconSVG []byte

func main() {
	debug := os.Getenv("FYNETUNE_DEBUG") == "1"
	configureLogging(debug)
	currentAppID := appID
	if debug {
		currentAppID = debugAppID
	}
	a := app.NewWithID(currentAppID)
	if debug {
		if err := cache.Clear(a.Cache()); err != nil {
			slog.Warn("could not clear debug cache", "error", err)
		}
		storage.Clear(a.Preferences())
	}
	metadata := a.Metadata()
	metadata.ID = currentAppID
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

// configureLogging sets the minimum slog level for the current run.
func configureLogging(debug bool) {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	if debug {
		level.Set(slog.LevelDebug)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}
