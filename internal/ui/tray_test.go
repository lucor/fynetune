package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
)

// TestTrayPlaybackAfterRestart exercises the persisted selection through the tray action.
func TestTrayPlaybackAfterRestart(t *testing.T) {
	favorite := radio.Station{ID: "favorite", Name: "Favorite", URL: "https://example.com/favorite"}
	recent := radio.Station{ID: "recent", Name: "Recent", URL: "https://example.com/recent"}
	for _, scenario := range []struct {
		name     string
		selected string
		autoPlay bool
		want     radio.Station
	}{
		{name: "saved favorite", selected: favorite.ID, want: favorite},
		{name: "saved recent station", selected: recent.ID, want: recent},
		{name: "missing selection falls back to favorite", selected: "missing", want: favorite},
		{name: "automatic playback", selected: recent.ID, autoPlay: true, want: recent},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base := test.NewTempApp(t)
			driver := &startupTestDriver{Driver: base.Driver(), completed: make(chan func(), 1)}
			runtime := &startupTestApp{App: base, driver: driver}
			fyne.SetCurrentApp(runtime)
			t.Cleanup(func() { fyne.SetCurrentApp(base) })
			app := &trayTestApp{App: runtime}
			store := storage.New(app)
			if err := store.SaveStations([]radio.Station{favorite}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveRecent([]storage.RecentStation{{Station: recent}}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveSettings(storage.Settings{
				Selected: scenario.selected, DiscoveryCountry: "@IT", Volume: 0.75, AutoPlay: scenario.autoPlay,
			}); err != nil {
				t.Fatal(err)
			}
			player := &trayTestPlayer{events: make(chan radio.Event)}
			window := New(app, player, nil)
			waitStartup(t, driver.completed)
			t.Cleanup(func() { _ = window.Close() })
			if app.menu == nil || app.icon == nil || app.trayWindowSet {
				t.Fatal("tray menu and icon must be registered without assigning a tray window")
			}
			if len(app.trayCalls) < 2 || app.trayCalls[0] != "menu" || app.trayCalls[1] != "icon" {
				t.Fatalf("tray initialization order = %v, want menu before icon", app.trayCalls)
			}
			if got := app.menu.Items[0].Label; got != scenario.want.Name {
				t.Fatalf("tray station = %q, want %q", got, scenario.want.Name)
			}
			if got := window.playerBar.title.Text; got != scenario.want.Name {
				t.Fatalf("player station = %q, want %q", got, scenario.want.Name)
			}
			if scenario.autoPlay {
				if player.played.ID != scenario.want.ID || app.menu.Items[3].Label != "Stop" {
					t.Fatal("automatic playback must start the restored station and update the tray")
				}
				app.menu.Items[3].Action()
				if player.state != radio.StateStopped || app.menu.Items[3].Label != "Play" {
					t.Fatal("tray Stop must stop playback and become Play")
				}
				return
			}
			if player.played.ID != "" {
				t.Fatal("restoring a selection must not play it when automatic playback is disabled")
			}
			app.menu.Items[3].Action()
			if player.played.ID != scenario.want.ID || app.menu.Items[3].Label != "Stop" {
				t.Fatal("tray Play must start the restored station and become Stop")
			}
		})
	}
}

// trayTestApp records desktop tray registration while using Fyne's headless app.
type trayTestApp struct {
	fyne.App
	menu          *fyne.Menu
	icon          fyne.Resource
	trayWindowSet bool
	trayCalls     []string
}

func (a *trayTestApp) SetSystemTrayMenu(menu *fyne.Menu) {
	a.menu = menu
	a.trayCalls = append(a.trayCalls, "menu")
}
func (a *trayTestApp) SetSystemTrayIcon(icon fyne.Resource) {
	a.icon = icon
	a.trayCalls = append(a.trayCalls, "icon")
}
func (a *trayTestApp) SetSystemTrayWindow(fyne.Window) { a.trayWindowSet = true }

// trayTestPlayer records playback commands without opening streams or audio devices.
type trayTestPlayer struct {
	events chan radio.Event
	state  radio.PlayerState
	played radio.Station
}

func (p *trayTestPlayer) Play(station radio.Station) {
	p.played = station
	p.state = radio.StateConnecting
}
func (p *trayTestPlayer) Stop()                      { p.state = radio.StateStopped }
func (p *trayTestPlayer) SetVolume(float64)          {}
func (p *trayTestPlayer) SetReconnect(bool)          {}
func (p *trayTestPlayer) State() radio.PlayerState   { return p.state }
func (p *trayTestPlayer) Events() <-chan radio.Event { return p.events }
func (p *trayTestPlayer) Close() error               { return nil }
