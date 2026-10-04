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
desktop packages with `mise run package-macos`, `mise run package-linux`, or
`mise run package-windows`.
Build an Android development APK with `mise run package-android`. It installs
the SDK components through `android-setup` and writes `dist/FyneTune.apk`. Run
`mise install` first for the configured Java and Android SDK tools. See the
README for SDK overrides and APK signing behavior.

Fyne and Oto require platform development libraries; see the README for the
platform prerequisites. Keep public documentation, comments, and diagnostics in
plain, idiomatic English.
