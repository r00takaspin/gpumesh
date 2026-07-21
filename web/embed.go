package web

import "embed"

// TemplatesFS embeds the templates directory.
//
//go:embed templates/*
var TemplatesFS embed.FS
