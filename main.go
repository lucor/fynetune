// Package main wires the FyneTune desktop application and its runtime dependencies.
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
	appID                       = "dev.lucor.fynetune"
	appName                     = "FyneTune"
	appIconName                 = "icon.svg"
	streamResponseHeaderTimeout = 15 * time.Second
	radioBrowserRequestTimeout  = 15 * time.Second
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
	// Stream requests can remain open during playback, so this client limits only response-header wait time.
	streamClient := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: streamResponseHeaderTimeout}}
	// Radio Browser requests are short API calls, so this client has a total request timeout.
	radioBrowserHTTPClient := &http.Client{Timeout: radioBrowserRequestTimeout}
	// The radio engine connects stream transport, codec selection, and audio output.
	radioPlayer := radio.NewPlayer(httpstream.New(streamClient), codec.New(), otooutput.New())
	// The Radio Browser client supplies station directories to the Fyne UI.
	radioBrowserClient := radiobrowser.New(radioBrowserHTTPClient)
	window := ui.New(a, radioPlayer, radioBrowserClient)
	window.Run()
	if err := window.Close(); err != nil {
		slog.Error("could not close radio player", "error", err)
	}
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
