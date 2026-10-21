// Package frontend holds the pages of the desktop app: plain HTML, CSS and
// JavaScript, embedded so the executable is a single file. It is not behind a
// build tag so the tests that read the pages run on every platform.
package frontend

import "embed"

// Assets is served by the Wails asset server at the root of the window.
//
//go:embed index.html app.css app.js i18n.js ui.js wizard.js manager.js
var Assets embed.FS
