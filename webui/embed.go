package webui

import "embed"

//go:embed dist
//go:embed dist/*
//go:embed dist/assets/*
var Assets embed.FS
