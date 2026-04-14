package web

import "embed"

//go:embed static/css/style.css
//go:embed static/css/docs.min.css
//go:embed static/js/docs.min.js
//go:embed static/dist
//go:embed static/fonts
//go:embed static/images
var Assets embed.FS

//go:embed app/index.html
var SPAShell []byte

//go:embed api_docs.html
var APIDocsShell []byte
