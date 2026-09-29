// Package rental は運営提供のレンタルAIを実行する。
// 席ごとのワーカーが既存の参加プロトコルで内部WebSocketへ接続し、
// 応答が必要な要求だけをOpenAI Responses APIへ橋渡しする。
package rental

import (
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/aiwolfdial/aiwolf-nlp-server/room"
)

const (
	// maxWorkers は同時に動かせるレンタルAIの上限。待機中も数える。
	maxWorkers = 5
	// maxCallsPerSeat は1席あたりのAPI呼び出し上限（再試行込み）。
	maxCallsPerSeat = 60
	// maxConsecFailures は連続失敗でその試合を代替動作へ固定する閾値。
	maxConsecFailures = 3
)

var (
	ErrUnavailable = errors.New("レンタルAIは現在利用できません")
	ErrNoWorkers   = errors.New("レンタルAIの枠が一杯です")
)

// Manager は room.RentalDriver の実装。席ごとのワーカーと日次予算を管理する。
type Manager struct {
	wsURL  string
	client *openAIClient
	budget *Budget

	mu      sync.Mutex
	workers map[*room.Seat]*worker
}

// NewManager は環境変数から設定を読み、利用可否を判定する。
// OPENAI_API_KEY が無ければワーカーは作れるが Available() が false を返し、
// room 側がレンタル席の作成を拒否する。
func NewManager(cfg model.Config, apiKey string, dataDir string) *Manager {
	host := cfg.Server.WebSocket.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	wsURL := "ws://" + net.JoinHostPort(host, strconv.Itoa(cfg.Server.WebSocket.Port)) + "/ws"
	budget, err := NewBudget(dataDir)
	if err != nil {
		slog.Error("レンタル利用量の読み込みに失敗しました", "error", err)
		budget = NewBudgetInMemory()
	}
	m := &Manager{
		wsURL:   wsURL,
		budget:  budget,
		workers: map[*room.Seat]*worker{},
	}
	if apiKey != "" {
		m.client = newOpenAIClient(apiKey)
		slog.Info("レンタルAIを有効にしました", "model", modelName, "ws", wsURL)
	} else {
		slog.Warn("OPENAI_API_KEY が未設定のためレンタルAIは利用できません")
	}
	return m
}

// Available は利用可否とユーザー向け理由を返す。キー・内部理由は返さない。
func (m *Manager) Available() (bool, string) {
	if m.client == nil {
		return false, "レンタルAIは現在準備中です（管理者がAPIキーを設定すると有効になります）"
	}
	if m.budget.DailyCap() <= 0 {
		return false, "レンタルAIは現在停止中です"
	}
	if m.budget.DailyExceeded() {
		return false, "本日のレンタルAIの利用上限に達しました"
	}
	m.mu.Lock()
	full := len(m.workers) >= maxWorkers
	m.mu.Unlock()
	if full {
		return false, "レンタルAIの同時利用枠が一杯です。空くまでお待ちください"
	}
	return true, ""
}

// Attach は席へ内部ワーカーを接続する。接続の成否はonStateで非同期に報告する。
func (m *Manager) Attach(r *room.Room, seat *room.Seat, name, skill string, onState func(state, msg string)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client == nil || len(m.workers) >= maxWorkers {
		if m.client == nil {
			return ErrUnavailable
		}
		return ErrNoWorkers
	}
	if old := m.workers[seat]; old != nil {
		old.stop()
	}
	w := newWorker(m, r, seat, name, skill, seat.InternalToken, onState)
	m.workers[seat] = w
	go w.run()
	return nil
}

// Detach は席のワーカーを止める。未接続・既停止なら何もしない。
func (m *Manager) Detach(seat *room.Seat) {
	m.mu.Lock()
	w := m.workers[seat]
	delete(m.workers, seat)
	m.mu.Unlock()
	if w != nil {
		w.stop()
	}
}

// removeWorker はworker自身が終了するときにmapから外す。
func (m *Manager) removeWorker(seat *room.Seat, w *worker) {
	m.mu.Lock()
	if m.workers[seat] == w {
		delete(m.workers, seat)
	}
	m.mu.Unlock()
}

// Close はすべてのワーカーを止める。
func (m *Manager) Close() {
	m.mu.Lock()
	ws := make([]*worker, 0, len(m.workers))
	for _, w := range m.workers {
		ws = append(ws, w)
	}
	m.workers = map[*room.Seat]*worker{}
	m.mu.Unlock()
	for _, w := range ws {
		w.stop()
	}
}
