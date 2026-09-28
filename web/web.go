// Package web はWeb UIの静的ファイルを埋め込み、transportから配信する。
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
)

//go:embed static
var Static embed.FS

// AssetVersion はCSSとJSの内容から作り、CDNに古い画面が残るのを防ぐ。
var AssetVersion = assetVersion()

func assetVersion() string {
	css, err := Static.ReadFile("static/style.css")
	if err != nil {
		panic(err)
	}
	js, err := Static.ReadFile("static/app.js")
	if err != nil {
		panic(err)
	}
	hash := sha256.New()
	hash.Write(css)
	hash.Write(js)
	return hex.EncodeToString(hash.Sum(nil)[:8])
}
