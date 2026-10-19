# FyneTune contributor guidance

Work from the repository root. Use `mise` for setup, Go toolchain selection,
development tasks, and packaging. FyneTune supports the latest two Go releases:
`go.mod` sets the minimum supported release and `mise.toml` pins the latest.

Install the configured tools and dependencies with:

```console
mise install
mise run setup
```

Routine tasks:

```console
mise run fmt
mise run lint
mise run vuln
mise run test
mise run test-race
mise run build
mise run check
```

Go development tools are pinned in `go.mod` and invoked with `go tool`. Do not
install workflow tools using unpinned `go install ...@latest` commands. Create
a desktop package for the current operating system with `mise run package`.
Build a development Android APK with `mise run package-android`. The dependent
`mise run android-dex` task compiles the native playback Service to
`dist/fynetune.dex`; Fyne Tools merges it into its embedded DEX while packaging
`dist/FyneTune.apk`. This uses Fyne's debug packaging path and its current API-29
target. Android release packaging uses Fyne's release path and API-36 target.
Run `mise install` first for the configured Java and Android SDK tools. See the
README for SDK overrides.

Fyne and Oto require platform development libraries; see the README for the
platform prerequisites. Keep public documentation, comments, and diagnostics in
plain, idiomatic English.

Use Lucide for every new interface icon so the icon style stays consistent.
Add icons through `internal/ui/assets/icons/generate-icons.sh` and expose them
through `internal/ui/assets/icons/icons.go` rather than using one-off icon sources.

## Website

Run `mise run website-build` to copy the shared logo, favicon, and screenshot into
`website/`, or `mise run website-preview` to prepare and serve the page at
`http://127.0.0.1:8000`. The copied files are ignored by Git; edit their originals
in `internal/ui/assets/logo.svg`, `internal/ui/assets/icon.svg`, and
`assets/screenshot.png`.

The website workflow prepares assets for pull requests and deploys from `main` to GitHub
Pages. Set the repository's Pages publishing source to **GitHub Actions** before
the first deployment.

The workflow inserts the analytics snippet into the published HTML on `main`;
local previews and pull request builds do not load analytics. Edit the snippet
in `.github/workflows/website.yml` to change the tracking configuration.
