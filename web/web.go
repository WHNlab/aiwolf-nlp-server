// Package web はWeb UIの静的ファイルを埋め込み、transportから配信する。
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
)

//go:embed static
var Static embed.FS

// AssetVersion は分割したモジュール・素材も含め、更新時に一緒に切り替える。
var AssetVersion = assetVersion()

func assetVersion() string {
	hash := sha256.New()
	err := fs.WalkDir(Static, "static", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := Static.ReadFile(path)
		if err != nil {
			return err
		}
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(data)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(hash.Sum(nil)[:8])
}
