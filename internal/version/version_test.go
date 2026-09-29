package version

import (
	"runtime/debug"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
)

func TestCurrentBuildInformation(t *testing.T) {
	const revision = "a3f82c1a6a6f9ab7f6e0a5e9d0ccae4212345678"
	build := buildInfo(map[string]string{
		"vcs.revision": revision,
		"vcs.modified": "false",
		"vcs.time":     "2026-09-25T10:30:00Z",
	})
	got := current(fyne.AppMetadata{Version: "0.1.0"}, build)
	if got.Version != "dev-a3f82c1" || got.Commit != revision || got.Dirty || got.Date != "2026-09-25T10:30:00Z" {
		t.Fatalf("current build info = %+v", got)
	}
}

func TestVersionFormatting(t *testing.T) {
	const revision = "a3f82c1a6a6f9ab7f6e0a5e9d0ccae4212345678"
	tests := []struct {
		name    string
		base    string
		commit  string
		dirty   bool
		release bool
		want    string
	}{
		{name: "clean development", base: "0.1.0", commit: revision, want: "dev-a3f82c1"},
		{name: "dirty development", base: "0.1.0", commit: revision, dirty: true, want: "dev-a3f82c1.dirty"},
		{name: "short revision", base: "0.1.0", commit: "abcdef0", want: "dev-abcdef0"},
		{name: "missing revision", base: "0.1.0", want: "dev"},
		{name: "missing base version", commit: revision, want: "dev-a3f82c1"},
		{name: "missing base and revision", want: "dev"},
		{name: "metadata version ignored for development", base: "0.0.1", commit: revision, want: "dev-a3f82c1"},
		{name: "release metadata preserves version", base: "1.2.3", commit: revision, release: true, want: "1.2.3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := formatVersion(test.base, test.commit, test.dirty, test.release)
			if got != test.want {
				t.Errorf("version = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCurrentWithoutBuildInformation(t *testing.T) {
	got := current(fyne.AppMetadata{Version: "0.1.0"}, nil)
	if got.Version != "dev" || got.Commit != "" || got.Dirty || got.Date != "" {
		t.Fatalf("missing build information = %+v", got)
	}
}

func TestCurrentUsesDevelopmentVersionWithoutMetadata(t *testing.T) {
	got := current(fyne.AppMetadata{}, nil)
	if want := "dev"; got.Version != want {
		t.Fatalf("version without metadata = %q, want %q", got.Version, want)
	}
}

func TestUserAgent(t *testing.T) {
	SetApplicationMetadata(fyne.AppMetadata{Version: "1.2.3", Release: true})
	t.Cleanup(func() { SetApplicationMetadata(fyne.AppMetadata{}) })
	if got := UserAgent(); !strings.HasPrefix(got, "FyneTune/1.2.3 (+") || !strings.HasSuffix(got, website+")") {
		t.Errorf("UserAgent() = %q", got)
	}
}

func buildInfo(values map[string]string) *debug.BuildInfo {
	settings := make([]debug.BuildSetting, 0, len(values))
	for key, value := range values {
		settings = append(settings, debug.BuildSetting{Key: key, Value: value})
	}
	return &debug.BuildInfo{Settings: settings}
}
