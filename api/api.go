// Package api embeds the gateway's OpenAPI contract so tests (and tools) can read it.
package api

import _ "embed"

// OpenAPI is api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
