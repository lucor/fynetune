package main

import (
	_ "embed"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"go.lucor.dev/fynetune/internal/audio/otooutput"
	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/directory/radiobrowser"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/transport/httpstream"
	"go.lucor.dev/fynetune/internal/ui"
	"go.lucor.dev/fynetune/internal/version"
)

const appID = "dev.lucor.fynetune"

//go:embed internal/ui/assets/icon.svg
var appIconSVG []byte

func main() {
	a := app.NewWithID(appID)
	metadata := a.Metadata()
	metadata.ID = appID
	metadata.Name = "FyneTune"
	if !metadata.Release {
		metadata.Version = version.Current().Version
	}
	if metadata.Build < 1 {
		metadata.Build = 1
	}
	metadata.Icon = fyne.NewStaticResource("FyneTune-icon.svg", appIconSVG)
	app.SetMetadata(metadata)
	version.SetApplicationMetadata(a.Metadata())
	player := radio.NewPlayer(httpstream.New(nil), codec.New(), otooutput.New())
	window := ui.New(a, player, radiobrowser.New(nil))
	window.Run()
	if err := window.Close(); err != nil {
		log.Printf("close player: %v", err)
	}
}
