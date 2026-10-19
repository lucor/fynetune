//go:build !android

package android

import "go.lucor.dev/fynetune/internal/playback"

// Attach is a no-op when the app is not running on Android.
func Attach(*playback.Session) func() { return func() {} }
