package transport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/aiwolfdial/aiwolf-nlp-server/room"
	"github.com/aiwolfdial/aiwolf-nlp-server/web"
)

func TestInvitePublicURLAndKit(t *testing.T) {
	for _, tc := range []struct{ name, publicURL, host, want string }{
		{"proxy", "wss://zinro-ws.nyaolab.com/ws", "zinro.nyaolab.com", "wss://zinro-ws.nyaolab.com/ws?"},
		{"local", "", "localhost:8080", "ws://localhost:8081/ws?"},
		{"ipv6", "", "[::1]:8080", "ws://[::1]:8081/ws?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := model.LoadFromPath("../config/default_5.yml")
			if err != nil {
				t.Fatal(err)
			}
			cfg.Server.WebSocket.PublicURL = tc.publicURL
			s := &Server{config: *cfg, roomManager: room.NewManager(*cfg, nil)}
			sess, _ := s.roomManager.GetOrCreateSession("")
			r, err := s.roomManager.CreateRoom(sess, room.CreateParams{UserName: "所有者", AgentCount: 5})
			if err != nil {
				t.Fatal(err)
			}
			router := s.buildWebRouter()
			req := httptest.NewRequest("GET", "http://"+tc.host+"/api/v1/rooms/"+r.ID+"/invite?download=1", nil)
			req.Header.Set("Cookie", sessionCookieName+"="+sess.Token)
			req.Header.Set("X-Forwarded-Proto", "https")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			var invite map[string]any
			if err := json.Unmarshal(out.Body.Bytes(), &invite); err != nil {
				t.Fatal(err)
			}
			if out.Code != 200 || !strings.HasPrefix(invite["ws_url"].(string), tc.want) {
				t.Fatalf("unexpected invite: %d %v", out.Code, invite)
			}
			if invite["mode"] != "turn" || out.Header().Get("Cache-Control") != "no-store" || !strings.Contains(out.Header().Get("Content-Disposition"), "invite.json") {
				t.Fatal("招待設定の属性が不正です")
			}
			out = httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest("GET", "/api/v1/rooms/"+r.ID+"/invite", nil))
			if out.Code != 403 {
				t.Fatalf("他者が招待を取得できました: %d", out.Code)
			}
			out = httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest("GET", web.PlayerKitPath, nil))
			archive, err := zip.NewReader(bytes.NewReader(out.Body.Bytes()), int64(out.Body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if len(archive.File) != 5 {
				t.Fatalf("予期しない配布ファイル数: %d", len(archive.File))
			}
			for _, f := range archive.File {
				content, err := f.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(content)
				content.Close()
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(f.Name, "/VERSION") && strings.TrimSpace(string(data)) != web.PlayerKitVersion {
					t.Fatal("配布バージョンが不一致です")
				}
				if bytes.Contains(data, []byte(sess.Token)) {
					t.Fatal("配布物に秘密情報が含まれています")
				}
			}
			latest := httptest.NewRecorder()
			router.ServeHTTP(latest, httptest.NewRequest("GET", "/downloads/aiwolf-player.zip", nil))
			if latest.Code != 200 || latest.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(latest.Body.Bytes(), out.Body.Bytes()) {
				t.Fatal("トップページ用の参加キットURLが同じZIPを返しません")
			}
		})
	}
}

func TestInvalidPublicURL(t *testing.T) {
	for _, raw := range []string{"https://example.org/ws", "wss:///ws", "wss://example.org/ws?token=secret", "wss://user:pass@example.org/ws"} {
		cfg := model.Config{}
		cfg.Server.WebSocket.PublicURL = raw
		if _, err := NewServer(cfg); err == nil {
			t.Errorf("不正な公開URLを受理しました: %s", raw)
		}
	}
}
