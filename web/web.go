// Package web はWeb UIの静的ファイルを埋め込み、transportから配信する。
package web

import "embed"

//go:embed static
var Static embed.FS
