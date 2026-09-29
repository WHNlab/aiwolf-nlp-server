package rental

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aiwolfdial/aiwolf-nlp-server/room"
	"github.com/gorilla/websocket"
)

const (
	// apiCallTimeout は1回のOpenAI呼び出しの上限。
	apiCallTimeout = 15 * time.Second
	// actionSafetyMargin はゲームのaction期限より手前で応答を諦める余白。
	actionSafetyMargin = 3 * time.Second
	// dialTimeout は内部WS接続の上限。
	dialTimeout = 10 * time.Second
)

// 受信パケットの最小限の型。model.Packet はアンマーシャルに向かない
// （Requestが独自Marshal）ため、必要なフィールドだけを独自に復号する。
type packetIn struct {
	Request        string     `json:"request"`
	Info           *infoIn    `json:"info"`
	Setting        *settingIn `json:"setting"`
	TalkHistory    []talkIn   `json:"talk_history"`
	WhisperHistory []talkIn   `json:"whisper_history"`
}

type infoIn struct {
	Day            int               `json:"day"`
	Agent          json.RawMessage   `json:"agent"`
	RoleMap        map[string]string `json:"role_map"`
	StatusMap      map[string]string `json:"status_map"`
	DivineResult   *judgeIn          `json:"divine_result"`
	MediumResult   *judgeIn          `json:"medium_result"`
	ExecutedAgent  json.RawMessage   `json:"executed_agent"`
	AttackedAgent  json.RawMessage   `json:"attacked_agent"`
	VoteList       []voteIn          `json:"vote_list"`
	AttackVoteList []voteIn          `json:"attack_vote_list"`
	RemainCount    *int              `json:"remain_count"`
	RemainLength   *int              `json:"remain_length"`
	RemainSkip     *int              `json:"remain_skip"`
	OwnerMessages  []string          `json:"owner_messages"`
	KeyPhrase      string            `json:"key_phrase"`
	Profile        *string           `json:"profile"`
}

type settingIn struct {
	Timeout struct {
		Action     int64 `json:"action"`
		Response   int64 `json:"response"`
		Acceptable int64 `json:"acceptable"`
	} `json:"timeout"`
	Talk    talkSettingIn `json:"talk"`
	Whisper talkSettingIn `json:"whisper"`
	Vote    struct {
		AllowSelfVote bool `json:"allow_self_vote"`
	} `json:"vote"`
	AttackVote struct {
		AllowSelfVote bool `json:"allow_self_vote"`
		AllowNoTarget bool `json:"allow_no_target"`
	} `json:"attack_vote"`
}

type talkSettingIn struct {
	Duration  *int64 `json:"duration"`
	MaxLength struct {
		PerTalk  int `json:"per_talk"`
		PerAgent int `json:"per_agent"`
	} `json:"max_length"`
}

type talkIn struct {
	Day   int    `json:"day"`
	Turn  int    `json:"turn"`
	Agent string `json:"agent"`
	Text  string `json:"text"`
	Skip  bool   `json:"skip"`
	Over  bool   `json:"over"`
}

type judgeIn struct {
	Day    int    `json:"day"`
	Agent  string `json:"agent"`
	Target string `json:"target"`
	Result string `json:"result"`
}

type voteIn struct {
	Day    int    `json:"day"`
	Agent  string `json:"agent"`
	Target string `json:"target"`
}

var actionRequests = map[string]bool{
	"TALK": true, "WHISPER": true, "VOTE": true,
	"DIVINE": true, "GUARD": true, "ATTACK": true,
}

type worker struct {
	mgr     *Manager
	room    *room.Room
	seat    *room.Seat
	name    string
	skill   string
	onState func(state, msg string)

	ctx    context.Context
	cancel context.CancelFunc
	conn   *websocket.Conn
	token  string // 接続時に確定した内部トークン。再Attachと区別する

	mu       sync.Mutex
	started  bool // INITIALIZE を受けて試合が始まった
	finished bool
	degraded bool // API失敗が続いたため代替動作へ固定
	calls    int  // この席のAPI呼び出し回数（再試行込み）
	failures int  // 連続失敗回数
	setting  settingIn
	info     infoIn // 直近のinfo
	talks    []talkIn
	whispers []talkIn
}

func newWorker(m *Manager, r *room.Room, seat *room.Seat, name, skill, token string, onState func(state, msg string)) *worker {
	ctx, cancel := context.WithCancel(context.Background())
	// NAME応答は改行までを名前として読まれるため、制御文字は空白へ置き換える。
	name = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r < 0x20 {
			return ' '
		}
		return r
	}, name)
	if strings.TrimSpace(name) == "" {
		name = "レンタルAI"
	}
	return &worker{
		mgr: m, room: r, seat: seat, name: name, skill: skill,
		token: token, onState: onState, ctx: ctx, cancel: cancel,
	}
}

// stop はワーカーを中断する。接続を閉じれば読み取りループが抜ける。
func (w *worker) stop() {
	w.cancel()
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func (w *worker) setState(state, msg string) {
	w.mgr.mu.Lock()
	current := w.mgr.workers[w.seat] == w
	w.mgr.mu.Unlock()
	if !current {
		return
	}
	if w.onState != nil {
		w.onState(state, msg)
	}
}

// dialURL は席専用の接続URLを組み立てる。seatのTokenは生成時に確定して以後変わらない。
func (w *worker) dialURL() string {
	sep := "?"
	if strings.Contains(w.mgr.wsURL, "?") {
		sep = "&"
	}
	return w.mgr.wsURL + sep + "room_id=" + w.room.ID + "&seat_token=" + w.seat.Token
}

func (w *worker) run() {
	defer w.mgr.removeWorker(w.seat, w)
	header := http.Header{}
	header.Set("X-Rental-Token", w.token)
	dialer := websocket.Dialer{HandshakeTimeout: dialTimeout}
	conn, resp, err := dialer.DialContext(w.ctx, w.dialURL(), header)
	if err != nil {
		if w.ctx.Err() == nil {
			slog.Warn("レンタルAIの接続に失敗しました", "room", w.room.ID, "error", err)
			w.setState("failed", "レンタルAIを接続できませんでした")
		}
		return
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()
	defer conn.Close()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if w.ctx.Err() == nil {
				w.mu.Lock()
				done := w.finished
				w.mu.Unlock()
				if !done {
					w.setState("failed", "レンタルAIの接続が切れました")
				}
			}
			return
		}
		var p packetIn
		if err := json.Unmarshal(data, &p); err != nil {
			slog.Warn("不明なパケットを受信しました", "room", w.room.ID, "error", err)
			continue
		}
		w.handle(&p)
		if w.ctx.Err() != nil {
			return
		}
	}
}

// handle は1パケットを処理する。action系のみAPI呼び出しを行う。
// 応答を待つ間もサーバは順に処理を進めるため、ここでブロックしてよい。
func (w *worker) handle(p *packetIn) {
	switch p.Request {
	case "NAME":
		// 生存確認はローカルで即時応答する。AI呼び出しは行わない。
		w.send(w.name)
		if !w.isStarted() {
			w.setState("ready", "")
		}
		return
	case "FINISH":
		w.mu.Lock()
		w.finished = true
		w.mu.Unlock()
		w.setState("finished", "")
		return
	}

	w.mu.Lock()
	if p.Setting != nil {
		w.setting = *p.Setting
	}
	if p.Info != nil {
		w.info = *p.Info
	}
	if len(p.TalkHistory) > 0 {
		w.talks = append(w.talks, p.TalkHistory...)
	}
	if len(p.WhisperHistory) > 0 {
		w.whispers = append(w.whispers, p.WhisperHistory...)
	}
	if p.Request == "INITIALIZE" {
		w.started = true
	}
	w.mu.Unlock()

	if p.Request == "INITIALIZE" {
		w.setState("playing", "")
	}
	if !actionRequests[p.Request] {
		return
	}
	w.act(p.Request)
}

func (w *worker) isStarted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.started
}

// send は応答本文をそのまま返す。
func (w *worker) send(text string) {
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn == nil {
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(text)); err != nil {
		slog.Warn("レンタルAIの応答送信に失敗しました", "room", w.room.ID, "error", err)
	}
}

// sendResponse は応答本文と所有者向けメモを {"response","note"} で返す。
func (w *worker) sendResponse(text, note string) {
	if note == "" {
		w.send(text)
		return
	}
	payload, err := json.Marshal(map[string]string{"response": text, "note": note})
	if err != nil {
		w.send(text)
		return
	}
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn != nil {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			slog.Warn("レンタルAIの応答送信に失敗しました", "room", w.room.ID, "error", err)
		}
	}
}

// act は要求への応答を生成して送信する。失敗時は合法な代替応答を1回だけ返す。
func (w *worker) act(request string) {
	w.mu.Lock()
	setting := w.setting
	degraded := w.degraded
	calls := w.calls
	w.mu.Unlock()

	actionMs := setting.Timeout.Action
	if actionMs <= 0 {
		actionMs = 30000 // 設定未到着時の保守的な既定値
	}
	deadline := time.Now().Add(time.Duration(actionMs) * time.Millisecond)
	underMatchCap := w.mgr.budget.MatchSpend(w.room.ID) < w.mgr.budget.MatchCap()
	canCall := !degraded && calls < maxCallsPerSeat && underMatchCap
	reserved := false
	if canCall {
		reserved = w.mgr.budget.TryReserve()
	}
	if !degraded && calls > 0 && (!canCall || !reserved) {
		// これまで呼び出せていたのに止まった=上限到達。1回だけ所有者へ知らせる。
		w.mu.Lock()
		w.degraded = true
		w.mu.Unlock()
		w.setState("degraded", "レンタルAIの利用上限に達したため、以後は自動の代替行動に切り替わりました")
	}
	if canCall && reserved {
		out, usage, err := w.mgr.client.generate(w.ctx, buildPrompt(w, request), schemaFor(request, w.targets(request)), deadline)
		w.mgr.budget.Settle(usage, w.room.ID)
		w.mu.Lock()
		w.calls++
		if err != nil {
			w.failures++
			if w.failures >= maxConsecFailures {
				w.degraded = true
				w.setState("degraded", "AIが応答できなかったため、今回の試合では自動の代替行動に切り替えました")
			}
		} else {
			w.failures = 0
		}
		w.mu.Unlock()
		if err == nil && out != nil {
			if request == "TALK" || request == "WHISPER" {
				w.sendResponse(validSpeech(out.speech, &setting, request, w.info.RemainSkip), validNote(out.note))
			} else {
				w.sendResponse(validTarget(out.target, w.targets(request), request, &setting), validNote(out.note))
			}
			return
		}
		slog.Warn("レンタルAIの生成に失敗しました", "room", w.room.ID, "request", request, "error", err)
	}

	// 代替応答。発言はOver、対象系は合法な候補からランダムに選ぶ。
	if request == "TALK" || request == "WHISPER" {
		w.send("Over")
		return
	}
	cands := w.targets(request)
	if len(cands) == 0 {
		w.send("None")
		return
	}
	w.send(cands[rand.Intn(len(cands))])
}

// targets は要求種別に応じた合法な対象候補を返す。infoのstatus_mapから生存者を取る。
func (w *worker) targets(request string) []string {
	w.mu.Lock()
	info := w.info
	setting := w.setting
	w.mu.Unlock()
	self := agentName(info.Agent)
	alive := make([]string, 0, len(info.StatusMap))
	for name, st := range info.StatusMap {
		if st == "ALIVE" {
			alive = append(alive, name)
		}
	}
	sort.Strings(alive)
	out := make([]string, 0, len(alive))
	for _, name := range alive {
		switch request {
		case "VOTE":
			if !setting.Vote.AllowSelfVote && name == self {
				continue
			}
		case "ATTACK":
			if !setting.AttackVote.AllowSelfVote && name == self {
				continue
			}
			// 仲間の人狼を襲撃対象にしない（role_mapには自分の仲間だけ含まれる）。
			if info.RoleMap[name] == "WEREWOLF" {
				continue
			}
		case "DIVINE", "GUARD":
			// 自分自身は占い・護衛の対象にできない。
			if name == self {
				continue
			}
		}
		out = append(out, name)
	}
	return out
}

func agentName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// validSpeech は発言をゲームの制約に合わせて整形する。
// remainSkip が0なら Skip を Over へ丸める（残りスキップ枠を使い切っているため）。
func validSpeech(s string, setting *settingIn, request string, remainSkip *int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Over"
	}
	if s == "Skip" && remainSkip != nil && *remainSkip <= 0 {
		return "Over"
	}
	limit := 200
	ts := setting.Talk
	if request == "WHISPER" {
		ts = setting.Whisper
	}
	if ts.MaxLength.PerTalk > 0 && ts.MaxLength.PerTalk < limit {
		limit = ts.MaxLength.PerTalk
	}
	runes := []rune(s)
	if len(runes) > limit {
		s = string(runes[:limit])
	}
	return s
}

// validTarget は対象選択を合法な候補に丸める。enumに無い値は候補から選び直す。
func validTarget(s string, candidates []string, request string, setting *settingIn) string {
	s = strings.TrimSpace(s)
	if request == "ATTACK" && setting.AttackVote.AllowNoTarget && s == "None" {
		return "None"
	}
	for _, c := range candidates {
		if c == s {
			return s
		}
	}
	if len(candidates) == 0 {
		return "None"
	}
	return candidates[rand.Intn(len(candidates))]
}

// validNote は所有者向けメモの長さを丸める。noteはゲームへの応答ではない。
func validNote(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > 300 {
		s = string(runes[:300]) + "…"
	}
	return s
}
