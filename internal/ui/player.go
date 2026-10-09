package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
)

const (
	playerControlSize     = 44
	playerControlIconSize = 20
)

// playerWidget is the persistent compact player shown above the app navigation.
type playerWidget struct {
	*fyne.Container
	artwork   *canvas.Image
	title     *widget.Label
	track     *widget.Label
	play      *widget.Button
	volume    *widget.Slider
	volumePct *widget.Label
	mute      *widget.Button
	volumeRow *fyne.Container
	expanded  bool
}

type playerControlTheme struct {
	fyne.Theme
}

func (t playerControlTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameButtonRadius:
		return playerControlSize / 2
	case theme.SizeNameInlineIcon:
		return playerControlIconSize
	default:
		return t.Theme.Size(name)
	}
}

// newPlayerWidget builds the player layout with standard Fyne widgets.
func newPlayerWidget(baseTheme fyne.Theme, volume float64, onToggle func(), onVolumeChanged func(float64), onMute func()) *playerWidget {
	artwork := newStationArtwork()

	title := widget.NewLabelWithStyle("Choose a station", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis
	track := widget.NewLabel("Not playing")
	track.Importance = widget.LowImportance
	track.SizeName = stationMetadataSizeName
	track.Truncation = fyne.TextTruncateEllipsis
	play := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), onToggle)
	play.Importance = widget.HighImportance
	play.Disable()

	playerText := container.NewPadded(container.NewVBox(title, track))
	centeredText := container.NewThemeOverride(
		container.NewVBox(layout.NewSpacer(), playerText, layout.NewSpacer()),
		compactTextTheme{Theme: baseTheme},
	)
	playControl := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(playerControlSize),
		container.NewThemeOverride(play, playerControlTheme{Theme: baseTheme})))
	content := container.NewBorder(
		nil, nil,
		container.NewCenter(artwork),
		playControl,
		centeredText,
	)
	p := &playerWidget{
		artwork:   artwork,
		title:     title,
		track:     track,
		play:      play,
		volume:    widget.NewSlider(0, 1),
		volumePct: widget.NewLabel(""),
		mute:      widget.NewButtonWithIcon("", theme.VolumeUpIcon(), onMute),
	}
	p.volume.Step = 0.01
	p.volume.Value = volume
	p.volume.OnChanged = onVolumeChanged
	p.volumePct.Alignment = fyne.TextAlignTrailing
	p.mute.Importance = widget.LowImportance
	p.volumeRow = container.NewBorder(
		nil, nil,
		container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(playerControlSize), p.mute)),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(48, playerControlSize), p.volumePct)),
		p.volume,
	)
	p.volumeRow.Hide()
	compact := newPlayerTapArea(content, p.toggleVolume)
	p.Container = container.NewVBox(widget.NewSeparator(), compact, p.volumeRow)
	p.setVolumeState(volume, false)
	return p
}

// toggleVolume shows or hides the volume controls beneath the compact player.
func (p *playerWidget) toggleVolume() {
	p.expanded = !p.expanded
	if p.expanded {
		p.volumeRow.Show()
	} else {
		p.volumeRow.Hide()
	}
	p.Refresh()
}

// setVolumeState synchronizes the slider, percentage, and mute icon.
func (p *playerWidget) setVolumeState(volume float64, muted bool) {
	p.volume.Value = volume
	p.volume.Refresh()
	actual := volume
	if muted {
		actual = 0
	}
	p.volumePct.SetText(fmt.Sprintf("%d%%", int(actual*100+0.5)))
	if actual == 0 {
		p.mute.SetIcon(theme.VolumeMuteIcon())
	} else {
		p.mute.SetIcon(theme.VolumeUpIcon())
	}
}

// setPlayback displays the selected station and current playback state.
func (p *playerWidget) setPlayback(station radio.Station, status string, playing bool) {
	if station.ID == "" {
		p.title.SetText("Choose a station")
		p.play.Disable()
	} else {
		p.title.SetText(station.Name)
		p.play.Enable()
	}
	p.track.SetText(status)
	if playing {
		p.play.SetIcon(theme.MediaStopIcon())
	} else {
		p.play.SetIcon(theme.MediaPlayIcon())
	}
}

// playerTapArea makes the compact player details open the volume controls.
type playerTapArea struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	onTapped func()
}

func newPlayerTapArea(content fyne.CanvasObject, onTapped func()) *playerTapArea {
	area := &playerTapArea{content: content, onTapped: onTapped}
	area.ExtendBaseWidget(area)
	return area
}

// CreateRenderer displays the compact player content as a tappable area.
func (a *playerTapArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(a.content)
}

// Tapped toggles the expanded volume controls.
func (a *playerTapArea) Tapped(*fyne.PointEvent) {
	if a.onTapped != nil {
		a.onTapped()
	}
}
