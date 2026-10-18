// Package icons provides embedded Lucide PNGs as Fyne resources.
package icons

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed active/*.png danger/*.png dark/*.png light/*.png
var assets embed.FS

var resources = loadResources()

// loadResources embeds every generated PNG once and keeps a reusable Fyne resource for it.
func loadResources() map[string]fyne.Resource {
	paths, err := fs.Glob(assets, "*/*.png")
	if err != nil {
		panic(fmt.Sprintf("list embedded Lucide icons: %v", err))
	}
	resources := make(map[string]fyne.Resource, len(paths))
	for _, path := range paths {
		content, err := assets.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("read embedded Lucide icon %q: %v", path, err))
		}
		name := "lucide-" + strings.ReplaceAll(strings.TrimSuffix(path, ".png"), "/", "-") + ".png"
		resources[strings.TrimSuffix(path, ".png")] = fyne.NewStaticResource(name, content)
	}
	return resources
}

// themed selects a light or dark PNG from the active Fyne theme colors.
func themed(name string) fyne.Resource {
	variant := "light"
	if app := fyne.CurrentApp(); app != nil {
		background := app.Settings().Theme().Color(theme.ColorNameBackground, app.Settings().ThemeVariant())
		r, g, b, _ := background.RGBA()
		if uint64(r)+uint64(g)+uint64(b) < 3*0x8000 {
			variant = "dark"
		}
	}
	return resource(variant + "/" + name)
}

// active returns a blue icon variant for selected UI states.
func active(name string) fyne.Resource {
	return resource("active/" + name)
}

func resource(name string) fyne.Resource {
	icon, ok := resources[name]
	if !ok {
		panic(fmt.Sprintf("Lucide icon %q is not embedded", name))
	}
	return icon
}

// Favorites returns the theme-colored outline heart icon.
func Favorites() fyne.Resource { return themed("heart") }

// FavoritesActive returns the blue heart icon for selected favorites.
func FavoritesActive() fyne.Resource { return active("heart") }

// Recent returns the theme-colored recent history icon.
func Recent() fyne.Resource { return themed("rotate-ccw-clock") }

// RecentActive returns the blue recent history icon.
func RecentActive() fyne.Resource { return active("rotate-ccw-clock") }

// Discover returns the theme-colored telescope icon.
func Discover() fyne.Resource { return themed("telescope") }

// DiscoverActive returns the blue telescope icon.
func DiscoverActive() fyne.Resource { return active("telescope") }

// Settings returns the theme-colored settings icon.
func Settings() fyne.Resource { return themed("settings") }

// SettingsActive returns the blue settings icon.
func SettingsActive() fyne.Resource { return active("settings") }

// Palette returns the theme-colored palette icon.
func Palette() fyne.Resource { return themed("palette") }

// PaletteActive returns the blue palette icon.
func PaletteActive() fyne.Resource { return active("palette") }

// Appearance returns the theme-colored sun and moon icon.
func Appearance() fyne.Resource { return themed("sun-moon") }

// Playback returns the theme-colored circled play icon.
func Playback() fyne.Resource { return themed("circle-play") }

// Play returns the theme-colored play icon.
func Play() fyne.Resource { return themed("play") }

// Stop returns the theme-colored power control icon.
func Stop() fyne.Resource { return themed("circle-power") }

// Reconnect returns the theme-colored repeat icon.
func Reconnect() fyne.Resource { return themed("repeat") }

// Volume returns the theme-colored volume icon.
func Volume() fyne.Resource { return themed("volume-2") }

// VolumeMuted returns the theme-colored muted volume icon.
func VolumeMuted() fyne.Resource { return themed("volume") }

// PictureInPicture returns the theme-colored picture-in-picture icon.
func PictureInPicture() fyne.Resource { return themed("picture-in-picture") }

// Info returns the theme-colored information icon.
func Info() fyne.Resource { return themed("info") }

// MoreVertical returns the theme-colored vertical overflow icon.
func MoreVertical() fyne.Resource { return themed("ellipsis-vertical") }

// Pencil returns the theme-colored edit icon.
func Pencil() fyne.Resource { return themed("pencil") }

// Trash returns the theme-colored delete icon.
func Trash() fyne.Resource { return themed("trash") }

// TrashDanger returns the delete icon in the app's destructive-action color.
func TrashDanger() fyne.Resource { return resource("danger/trash") }

// Plus returns the theme-colored add icon.
func Plus() fyne.Resource { return themed("plus") }

// ExternalLink returns the theme-colored external link icon.
func ExternalLink() fyne.Resource { return themed("external-link") }

// ExternalLinkActive returns the blue external link icon.
func ExternalLinkActive() fyne.Resource { return active("external-link") }

// Power returns the theme-colored power icon.
func Power() fyne.Resource { return themed("power") }
