// Package web embeds the Kuraka web panel.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// FS returns the panel files with index.html at the root.
func FS() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
