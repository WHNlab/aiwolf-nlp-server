package model

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// SeatContext はルーム参加席と接続を結びつけるメタ情報。
// Webモードの /ws?seat_token=... で接続したエージェントにのみ設定される。
type SeatContext struct {
	RoomID    string
	SeatID    string
	KeyPhrase string
	Inbox     *OwnerInbox
}

// OwnerInbox は人間ユーザーからの助言を詰む箱。
// logic がパケット送信のタイミングで取り出して info.owner_messages へ同梱する。
// ポインタで共有することで、ゲーム側にルームの概念を持ち込まない。
type OwnerInbox struct {
	mu       sync.Mutex
	messages []string
}

func NewOwnerInbox() *OwnerInbox {
	return &OwnerInbox{}
}

func (b *OwnerInbox) Push(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, message)
}

// Drain は詰んでいるメッセージをすべて取り出し、箱を空にする。
func (b *OwnerInbox) Drain() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.messages) == 0 {
		return nil
	}
	out := b.messages
	b.messages = nil
	return out
}

type Connection struct {
	TeamName     string
	OriginalName string
	Conn         *websocket.Conn
	Header       *http.Header
	Seat         *SeatContext
}

func NewConnection(conn *websocket.Conn, header *http.Header) (*Connection, error) {
	req, err := json.Marshal(Packet{
		Request: &R_NAME,
	})
	if err != nil {
		slog.Error("NAMEパケットの作成に失敗しました", "error", err)
		return nil, err
	}
	err = conn.WriteMessage(websocket.TextMessage, req)
	if err != nil {
		slog.Error("NAMEパケットの送信に失敗しました", "error", err)
		return nil, err
	}
	slog.Info("NAMEパケットを送信しました", "remote_addr", conn.RemoteAddr().String())
	_, res, err := conn.ReadMessage()
	if err != nil {
		slog.Error("NAMEリクエストの受信に失敗しました", "error", err)
		return nil, err
	}
	originalName := strings.TrimRight(string(res), "\n")
	teamName := strings.TrimRight(originalName, "1234567890")
	connection := Connection{
		TeamName:     teamName,
		OriginalName: originalName,
		Conn:         conn,
		Header:       header,
	}
	slog.Info("クライアントが接続しました", "team_name", connection.TeamName, "original_name", connection.OriginalName, "remote_addr", conn.RemoteAddr().String())
	return &connection, nil
}
