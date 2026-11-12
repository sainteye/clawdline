// Package extension carries the Clawdline Chrome extension inside the
// clawdline binary, so `clawdline browser launch` can write it next to the
// daemon and load it into Clawdline's own Chromium (docs/browser.md).
package extension

import "embed"

// Chrome is extension/chrome, including its _locales directory.
//
//go:embed all:chrome
var Chrome embed.FS
