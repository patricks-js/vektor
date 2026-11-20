//go:build webui

package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/index.html dist/assets/*
var assets embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return handler(dist)
}
