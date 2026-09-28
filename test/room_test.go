package test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/aiwolfdial/aiwolf-nlp-server/transport"
	"github.com/gorilla/websocket"
)

// Webルーム経由の対戦を端から端まで検証する。
// 5人の人間がそれぞれAIを接続し、キーフレーズで視点を解放し、1試合を完走させる。
func TestRoomGameEndToEnd(t *testing.T) {
	config, err := model.LoadFromPath("./config/room.yml")
	if err != nil {
		t.Fatalf("設定ファイルの読み込みに失敗しました: %v", err)
	}
	config.Server.WebSocket.Port = getAvailableTcpPort(config.Server.WebSocket.Host)
	config.Server.Web.Port = getAvailableTcpPort(config.Server.Web.Host)

	go func() {
		srv, err := transport.NewServer(*config)
		if err != nil {
			t.Logf("サーバ起動失敗: %v", err)
			return
		}
		srv.Run()
	}()
	time.Sleep(1 * time.Second)

	webBase := "http://" + config.Server.Web.Host + ":" + strconv.Itoa(config.Server.Web.Port)

	clients := make([]*http.Client, 5)
	for i := range clients {
		jar, _ := cookiejar.New(nil)
		clients[i] = &http.Client{Jar: jar, Timeout: 10 * time.Second}
	}

	post := func(c *http.Client, path string, body string) map[string]any {
		resp, err := c.Post(webBase+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST %s 失敗: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out map[string]any
		json.Unmarshal(b, &out)
		return out
	}
	get := func(c *http.Client, path string) map[string]any {
		resp, err := c.Get(webBase + path)
		if err != nil {
			t.Fatalf("GET %s 失敗: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out map[string]any
		json.Unmarshal(b, &out)
		return out
	}

	created := post(clients[0], "/api/v1/rooms",
		`{"room_name":"テスト村","user_name":"ホスト","agent_count":5,"mode":"participate"}`)
	roomID, _ := created["room_id"].(string)
	if roomID == "" {
		t.Fatalf("ルーム作成に失敗しました: %v", created)
	}

	for i := 1; i < 5; i++ {
		res := post(clients[i], "/api/v1/rooms/"+roomID+"/join",
			fmt.Sprintf(`{"name":"参加%d","mode":"participate"}`, i))
		if res["error"] != nil {
			t.Fatalf("参加失敗: %v", res)
		}
	}

	keyPhrases := make(chan string, 5)
	pongs := make(chan string, 1)
	var wsConns []*websocket.Conn
	for i := 0; i < 5; i++ {
		inv := get(clients[i], "/api/v1/rooms/"+roomID+"/invite")
		wsURL, _ := inv["ws_url"].(string)
		if wsURL == "" {
			t.Fatalf("招待情報が取れません: %v", inv)
		}
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("AI接続失敗: %v", err)
		}
		if i == 0 {
			conn.SetPongHandler(func(data string) error {
				pongs <- data
				return nil
			})
		}
		wsConns = append(wsConns, conn)
		idx := i
		go func(c *websocket.Conn, n int) {
			for {
				_, msg, err := c.ReadMessage()
				if err != nil {
					return
				}
				var pkt map[string]any
				if json.Unmarshal(msg, &pkt) != nil {
					continue
				}
				if pkt["request"] == "NAME" {
					c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("team-%d", n)))
					continue
				}
				if pkt["request"] == "INITIALIZE" {
					if info, ok := pkt["info"].(map[string]any); ok {
						if kp, ok := info["key_phrase"].(string); ok {
							keyPhrases <- kp
						}
					}
					continue
				}
				req, _ := pkt["request"].(string)
				switch req {
				case "TALK", "WHISPER":
					if n == 4 {
						// 開始直後の検証（claim等）のため1体だけ応答を遅らせる。
						time.Sleep(1500 * time.Millisecond)
					}
					if n == 0 {
						c.WriteMessage(websocket.TextMessage, []byte(`{"response":"こんにちは","note":"応答テスト"}`))
					} else {
						c.WriteMessage(websocket.TextMessage, []byte("Over"))
					}
				case "VOTE", "DIVINE", "GUARD", "ATTACK":
					c.WriteMessage(websocket.TextMessage, []byte("Agent[01]"))
				default:
					c.WriteMessage(websocket.TextMessage, []byte(""))
				}
			}
		}(conn, idx)
	}

	deadline := time.Now().Add(10 * time.Second)
	rm := map[string]any{}
	for time.Now().Before(deadline) {
		rm = get(clients[0], "/api/v1/rooms/"+roomID)
		if c, _ := rm["connected"].(float64); int(c) == 5 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	rm = get(clients[0], "/api/v1/rooms/"+roomID)
	if c, _ := rm["connected"].(float64); int(c) != 5 {
		t.Fatalf("AI接続が揃いません: %v", rm["connected"])
	}
	// 開始待ちでもサーバが Ping を読み Pong を返せることを確かめる。
	if err := wsConns[0].WriteControl(websocket.PingMessage, []byte("waiting"), time.Now().Add(2*time.Second)); err != nil {
		t.Fatalf("待機中のPing送信失敗: %v", err)
	}
	select {
	case data := <-pongs:
		if data != "waiting" {
			t.Fatalf("待機中のPongが不正です: %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("待機中のPongが返りませんでした")
	}

	res := post(clients[0], "/api/v1/rooms/"+roomID+"/start", `{}`)
	if st, _ := res["status"].(string); st != "running" && st != "starting" {
		t.Fatalf("開始失敗: %v", res)
	}

	phrases := map[string]bool{}
	for i := 0; i < 5; i++ {
		select {
		case kp := <-keyPhrases:
			phrases[kp] = true
		case <-time.After(15 * time.Second):
			t.Fatal("キーフレーズを受信できませんでした")
		}
	}
	// どのフレーズが誰のものか分からないため、全員が全フレーズを試す。
	// 正しい席以外は拒否され、自分の分だけ解放される。
	for i := 0; i < 5; i++ {
		for p := range phrases {
			post(clients[i], "/api/v1/rooms/"+roomID+"/claim", fmt.Sprintf(`{"phrase":%q}`, p))
		}
	}
	rm = get(clients[0], "/api/v1/rooms/"+roomID)
	if v, _ := rm["viewer"].(map[string]any); v["view_mode"] != "agent" {
		t.Errorf("ホストのview_modeがagentではありません: %v", v["view_mode"])
	}

	spectatorJar, _ := cookiejar.New(nil)
	spectator := &http.Client{Jar: spectatorJar, Timeout: 10 * time.Second}
	post(spectator, "/api/v1/rooms/"+roomID+"/join", `{"name":"観戦","mode":"spectate"}`)
	pub := get(spectator, "/api/v1/rooms/"+roomID)
	if v, _ := pub["viewer"].(map[string]any); v["view_mode"] != "public" {
		t.Errorf("観戦者のview_modeがpublicではありません: %v", v["view_mode"])
	}
	for _, s := range pub["seats"].([]any) {
		if m := s.(map[string]any); m["role"] != "非公開" {
			t.Errorf("観戦者に役職が見えています: %v", m["role"])
		}
	}

	deadline = time.Now().Add(4 * time.Minute)
	status := ""
	for time.Now().Before(deadline) {
		rm = get(spectator, "/api/v1/rooms/"+roomID)
		status, _ = rm["status"].(string)
		if status == "finished" || status == "aborted" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if status != "finished" && status != "aborted" {
		t.Fatalf("ゲームが終了しませんでした: %v", status)
	}

	final := get(spectator, "/api/v1/rooms/"+roomID)
	for _, s := range final["seats"].([]any) {
		m := s.(map[string]any)
		if m["role"] == "非公開" || m["role"] == "" {
			t.Errorf("終了後も役職が非公開です: %v", m)
		}
	}

	hist := get(clients[0], "/api/v1/rooms/"+roomID+"/history")
	found := false
	for _, e := range hist["events"].([]any) {
		if e.(map[string]any)["type"] == "owner_note" {
			found = true
		}
	}
	if !found {
		t.Error("owner_note（拡張応答のnote）が履歴にありません")
	}
	for _, c := range wsConns {
		c.Close()
	}
}

func TestRoomWaitingDisconnect(t *testing.T) {
	config, err := model.LoadFromPath("./config/room.yml")
	if err != nil {
		t.Fatal(err)
	}
	config.Server.WebSocket.Port = getAvailableTcpPort(config.Server.WebSocket.Host)
	config.Server.Web.Port = getAvailableTcpPort(config.Server.Web.Host)
	go func() {
		srv, err := transport.NewServer(*config)
		if err == nil {
			srv.Run()
		}
	}()
	time.Sleep(time.Second)
	base := "http://" + config.Server.Web.Host + ":" + strconv.Itoa(config.Server.Web.Port)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	request := func(method, path, body string) map[string]any {
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
			t.Fatalf("%s %s: %d", method, path, res.StatusCode)
		}
		var out map[string]any
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	created := request("POST", "/api/v1/rooms", `{"user_name":"ホスト","agent_count":5,"mode":"participate"}`)
	path := "/api/v1/rooms/" + created["room_id"].(string)
	invite := request("GET", path+"/invite", "")
	conn, _, err := websocket.DefaultDialer.Dial(invite["ws_url"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("test-agent")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if request("GET", path, "")["connected"] == float64(1) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if request("GET", path, "")["connected"] != float64(1) {
		t.Fatal("AIの接続が表示されません")
	}
	conn.Close()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if request("GET", path, "")["connected"] == float64(0) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("切断したAIの席が接続済みのままです")
}
