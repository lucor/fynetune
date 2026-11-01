// Package assets contains shared FyneTune branding.
package assets

import _ "embed"

// IconSVG is the application and station-placeholder icon. Treat it as immutable.
//
//go:embed icon.svg
var IconSVG []byte
