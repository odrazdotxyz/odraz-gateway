// Package vanilla is the reference web UI: plain JavaScript, no build step. It is also the living documentation of
// the gateway API: everything it does goes through the routes in api/openapi.yaml. A UI of your own replaces it by
// passing a folder to the gateway (-web-dir) or a file system (Config.WebFS).
package vanilla

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js app.css assets
var files embed.FS

// FS is the UI's files, rooted at index.html.
func FS() fs.FS { return files }
