package api

import "embed"

// Files contains CastDock's published HTTP API contract.
//
//go:embed openapi.yaml
var Files embed.FS
