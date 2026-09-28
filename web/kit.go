package web

import (
	"archive/zip"
	"bytes"
	"embed"
	"io/fs"
)

const PlayerKitVersion = "0.2.0"
const PlayerKitPath = "/downloads/aiwolf-player-" + PlayerKitVersion + ".zip"

// 検証時のキャッシュやローカル招待設定を配布物に混ぜないため、埋め込み対象を限定する。
//
//go:embed kit/aiwolf-player/SKILL.md kit/aiwolf-player/GUIDE.md kit/aiwolf-player/VERSION kit/aiwolf-player/requirements.txt kit/aiwolf-player/scripts/agent.py
var PlayerKit embed.FS

func PlayerKitArchive() ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	err := fs.WalkDir(PlayerKit, "kit", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := PlayerKit.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := w.Create(path[len("kit/"):])
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
