package model

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// SeatContext はルーム参加席と接続を結びつけるメタ情報。
// Webモードの /ws?seat_token=... で接続したエージェントにのみ設定される。
type SeatContext struct {
	RoomID    string
	SeatID    string
	BotName   string
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
	messages     chan AgentMessage
	done         chan struct{}
	ready        *atomic.Bool
}

// Done は待機中も含め、WebSocket の読み取りが終了したことを通知する。
func (c *Connection) Done() <-chan struct{} {
	return c.done
}

func NewConnection(conn *websocket.Conn, header *http.Header) (*Connection, error) {
	return newConnection(conn, header, false)
}

// NewRoomConnection はWeb席では末尾の数字も含めたNAMEをチーム名・Bot名にする。
func NewRoomConnection(conn *websocket.Conn, header *http.Header) (*Connection, error) {
	return newConnection(conn, header, true)
}

func newConnection(conn *websocket.Conn, header *http.Header, webRoom bool) (*Connection, error) {
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
	if webRoom {
		teamName, err = NormalizeBotName(originalName)
		if err != nil {
			return nil, err
		}
	}
	connection := Connection{
		TeamName:     teamName,
		OriginalName: originalName,
		Conn:         conn,
		Header:       header,
		messages:     make(chan AgentMessage, 100),
		done:         make(chan struct{}),
		ready:        &atomic.Bool{},
	}
	// 開始待ちにも Ping を読み、Pong を返す。ゲーム開始後も同じ読み手を使う。
	go connection.readMessages()
	slog.Info("クライアントが接続しました", "team_name", connection.TeamName, "original_name", connection.OriginalName, "remote_addr", conn.RemoteAddr().String())
	return &connection, nil
}

func (c *Connection) readMessages() {
	defer close(c.done)
	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			select {
			case c.messages <- AgentMessage{Err: err}:
			default:
			}
			return
		}
		// ゲーム開始前の本文は応答として扱わない。制御フレームは ReadMessage が処理する。
		if c.ready.Load() {
			c.messages <- AgentMessage{Data: data}
		}
	}
}
