package web

import "embed"

// Files contains the templates and static assets shipped with CastDock.
//
//go:embed templates/*.html static/*
var Files embed.FS
