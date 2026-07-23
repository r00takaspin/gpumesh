package web

import "embed"

// EmbeddedFS embeds templates and static files for single-binary deployment.
//
//go:embed templates/* static/*
var EmbeddedFS embed.FS
