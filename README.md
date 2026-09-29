# FyneTune

<p align="center">
  <img src="internal/ui/assets/logo.png" alt="FyneTune internet radio player" width="420">
</p>

FyneTune is a lightweight desktop internet radio player for Windows, macOS, and Linux. Discover stations, save favorites, and listen while the app runs in the background.

## Features

- Browse and search stations from [Radio Browser](https://www.radio-browser.info/).
- Save favorite stations, revisit recently played stations, or add a custom stream.
- Play direct MP3 streams with volume control and automatic reconnection.
- Show artist and track information when a station provides ICY metadata.
- Keep listening from the system tray on supported desktops.
- Save stations and preferences between launches.

FyneTune currently plays direct MP3 streams. Stations using AAC, HLS, or other formats are not supported yet.

## Development

FyneTune is written in Go and uses [Fyne](https://fyne.io/) for its desktop interface. [mise](https://mise.jdx.dev/) manages the Go toolchain and project tasks. Install the platform prerequisites below, then set up dependencies and run the app:

```sh
mise install
mise run setup
mise run run
```

Project tasks are available through mise:

- `mise run build` builds the desktop binary to `dist/fynetune`.
- `mise run licenses` manually generates `THIRD_PARTY_LICENSES` from Go module dependencies.
- `mise run package` creates a native package for the current platform.
- `mise run check` runs formatting, static analysis, vulnerability checks, tests, and a build.
- `mise run test-race` runs tests with Go's race detector.

The app uses the approved SVG icon at runtime; Fyne Tool packaging uses its manually exported PNG counterpart at `internal/ui/assets/icon.png`. Tests use local synthetic streams and do not require an audio device or public radio service.

### Platform prerequisites

To compile FyneTune for your target platform, install the prerequisites listed by
[Fyne](https://docs.fyne.io/started/) and [Oto](https://github.com/ebitengine/oto).
Requirements vary by operating system and target architecture, so follow both projects' platform-specific setup guidance.

## License

FyneTune is licensed under the [MIT License](LICENSE).
