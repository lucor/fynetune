// Package version reports application and source-control build information.
package version

import (
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
)

const website = "https://fynetune.lucor.dev"

var (
	metadataMu sync.RWMutex
	metadata   fyne.AppMetadata
)

// Info describes the application version and the source used to build it.
type Info struct {
	Version string
	Commit  string
	Dirty   bool
	Date    string
}

// SetApplicationMetadata records metadata from the initialized Fyne app. It
// must be called before using Current, normally immediately after app.New.
func SetApplicationMetadata(value fyne.AppMetadata) {
	metadataMu.Lock()
	metadata = value
	metadataMu.Unlock()
}

// Current combines Fyne release metadata with Go's embedded VCS information.
func Current() Info {
	metadataMu.RLock()
	appMetadata := metadata
	metadataMu.RUnlock()

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		buildInfo = nil
	}
	return current(appMetadata, buildInfo)
}

// UserAgent returns the shared HTTP User-Agent for FyneTune requests.
func UserAgent() string {
	return "FyneTune/" + Current().Version + " (+" + website + ")"
}

func current(app fyne.AppMetadata, build *debug.BuildInfo) Info {
	var revision, buildDate string
	var dirty bool
	if build != nil {
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				dirty = setting.Value == "true"
			case "vcs.time":
				buildDate = setting.Value
			}
		}
	}
	if parsed, err := time.Parse(time.RFC3339, buildDate); err == nil {
		buildDate = parsed.UTC().Format(time.RFC3339)
	} else {
		buildDate = ""
	}

	return Info{
		Version: formatVersion(app.Version, revision, dirty, app.Release),
		Commit:  revision,
		Dirty:   dirty,
		Date:    buildDate,
	}
}

func formatVersion(baseVersion, commit string, dirty, release bool) string {
	baseVersion = strings.TrimSpace(baseVersion)
	if release {
		return baseVersion
	}
	version := "dev"
	if short := ShortCommit(commit); short != "" {
		version += "-" + short
	}
	if dirty {
		version += ".dirty"
	}
	return version
}

// ShortCommit returns the seven-character display form of a Git revision.
func ShortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
