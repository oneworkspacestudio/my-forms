package web

import "embed"

//go:embed index.html app.js style.css logo.svg
var Files embed.FS
