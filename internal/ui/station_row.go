package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var _ desktop.Hoverable = (*stationTapArea)(nil)

type stationTapArea struct {
	widget.BaseWidget
	content      fyne.CanvasObject
	background   *canvas.Rectangle
	onTapped     func()
	onLongTapped func()
}

func newStationTapArea(content fyne.CanvasObject, onTapped, onLongTapped func()) *stationTapArea {
	area := &stationTapArea{content: content, onTapped: onTapped, onLongTapped: onLongTapped}
	area.ExtendBaseWidget(area)
	return area
}

func (a *stationTapArea) CreateRenderer() fyne.WidgetRenderer {
	a.background = canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(container.NewStack(a.background, a.content))
}

func (a *stationTapArea) Tapped(*fyne.PointEvent) {
	if a.onTapped != nil {
		a.onTapped()
	}
}

func (a *stationTapArea) LongTapped(*fyne.PointEvent) {
	if a.onLongTapped != nil {
		a.onLongTapped()
	}
}

func (a *stationTapArea) MouseIn(*desktop.MouseEvent) {
	if a.background == nil {
		return
	}
	a.background.FillColor = theme.Color(theme.ColorNameHover)
	a.background.Refresh()
}

func (a *stationTapArea) MouseMoved(*desktop.MouseEvent) {}

func (a *stationTapArea) MouseOut() {
	if a.background == nil {
		return
	}
	a.background.FillColor = color.Transparent
	a.background.Refresh()
}

func (a *stationTapArea) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}
