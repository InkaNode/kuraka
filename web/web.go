// Package web embeds the Kuraka web panel.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

//go:embed addons
var addonFiles embed.FS

// Addon returns one of the embedded Stalwart or Libredesk addons.
func Addon(name string) ([]byte, error) {
	switch name {
	case "stalwart.js", "stalwart.css", "desk.js", "desk.css":
		return addonFiles.ReadFile("addons/" + name)
	default:
		return nil, fs.ErrNotExist
	}
}

// FS returns the panel files with index.html at the root.
func FS() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
