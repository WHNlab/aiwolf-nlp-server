package room

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aiwolfdial/aiwolf-nlp-server/logic"
	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/aiwolfdial/aiwolf-nlp-server/observer"
	"github.com/oklog/ulid/v2"
)

// Manager はルーム・セッション・席トークンを管理し、ゲームの生成とイベント記録を担う。
// 既存の待機部屋とは別経路で、seat_token 付き接続のみをここに流す。
// ロック順: Manager.mu（セッション・購読・seatConns）→ Room.mu（ルーム状態）。
type Manager struct {
	base       model.Config // サーバ起動時の設定。ルームごとに人数だけ差し替えたコピーを使う。
	newObserve func() observer.GameObserver
	mu         sync.Mutex
	rooms      map[string]*Room
	sessions   map[string]*Session
	seatTokens sync.Map // トークン→*Seat。r.muの内側でも書き込めるようsync.Mapにする。
	seatConns  sync.Map // *Seat → *model.Connection（ゲーム開始時に束ねる）
}

func NewManager(base model.Config, observerFactory func() observer.GameObserver) *Manager {
	return &Manager{
		base:       base,
		newObserve: observerFactory,
		rooms:      map[string]*Room{},
		sessions:   map[string]*Session{},
	}
}

func genToken(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		// 擬似乱数にフォールバックしない。乱数生成の失敗は致命的なのでpanicにする。
		panic(err)
	}
	return hex.EncodeToString(b)
}

// ---- セッション ----

// GetOrCreateSession はCookie相当のトークンを解決する。無ければ新規発行する。
// 2番目の戻り値は新規発行されたか（Cookieを書き込むべきか）。
func (m *Manager) GetOrCreateSession(token string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token != "" {
		if s, ok := m.sessions[token]; ok {
			return s, false
		}
	}
	s := &Session{Token: genToken(16), UserID: "u-" + ulid.Make().String(), Created: time.Now()}
	m.sessions[s.Token] = s
	return s, true
}

func (m *Manager) Session(token string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[token]
}

// ---- 部屋 ----

type CreateParams struct {
	RoomName   string
	UserName   string
	AgentCount int
	Mode       string // "participate" or "spectate"
}

// CreateRoom は部屋を作り、作成者をホストとして登録する。
func (m *Manager) CreateRoom(sess *Session, p CreateParams) (*Room, error) {
	p.RoomName = strings.TrimSpace(p.RoomName)
	p.UserName = strings.TrimSpace(p.UserName)
	if p.RoomName == "" {
		p.RoomName = "人狼ルーム"
	}
	if len([]rune(p.RoomName)) > maxRoomNameLen {
		return nil, ErrRoomNameTooLong
	}
	if p.UserName == "" {
		return nil, ErrNameRequired
	}
	if len([]rune(p.UserName)) > maxUserNameLen {
		return nil, ErrNameTooLong
	}
	if _, ok := m.base.Logic.Roles[p.AgentCount]; !ok {
		return nil, ErrInvalidInput
	}
	cfg := m.base
	cfg.Game.AgentCount = p.AgentCount
	settings, err := model.NewSetting(cfg)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	sess.Name = p.UserName
	r := &Room{
		ID:        ulid.Make().String(),
		Name:      p.RoomName,
		Config:    cfg,
		AgentCnt:  p.AgentCount,
		Status:    StatusWaiting,
		HostID:    sess.UserID,
		Members:   map[string]*Member{},
		Events:    []*Event{},
		createdAt: time.Now(),
		subs:      map[int]*subscriber{},
		settings:  settings,
	}
	m.rooms[r.ID] = r
	m.mu.Unlock()

	r.mu.Lock()
	member := &Member{UserID: sess.UserID, Name: p.UserName, IsHost: true, Joined: time.Now()}
	r.Members[sess.UserID] = member
	if p.Mode != "spectate" {
		m.addSeatLocked(r, member)
	}
	m.addPlaceholderSeatsLocked(r)
	r.mu.Unlock()
	slog.Info("ルームを作成しました", "room", r.ID, "name", r.Name, "agents", r.AgentCnt)
	return r, nil
}

// addSeatLocked はメンバー用の席を一つ追加する。r.mu保持中に呼ぶ。
func (m *Manager) addSeatLocked(r *Room, member *Member) *Seat {
	seat := &Seat{
		ID:        "s" + ulid.Make().String(),
		UserID:    member.UserID,
		UserName:  member.Name,
		Token:     genToken(24),
		KeyPhrase: genToken(6), // 短めのフレーズ。AIが所有者へ伝え、Webで入力する。
		Inbox:     model.NewOwnerInbox(),
		Alive:     true,
	}
	r.Seats = append(r.Seats, seat)
	m.seatTokens.Store(seat.Token, seat)
	member.SeatID = seat.ID
	return seat
}

// addPlaceholderSeatsLocked は残りを空席として用意する。友達のAIがあとから座る。
func (m *Manager) addPlaceholderSeatsLocked(r *Room) {
	for len(r.Seats) < r.AgentCnt {
		seat := &Seat{
			ID:        "s" + ulid.Make().String(),
			Token:     genToken(24),
			KeyPhrase: genToken(6),
			Inbox:     model.NewOwnerInbox(),
			Alive:     true,
		}
		r.Seats = append(r.Seats, seat)
		m.seatTokens.Store(seat.Token, seat)
	}
}

func (m *Manager) GetRoom(id string) *Room {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rooms[id]
}

// Join はユーザーを部屋へ入室させる。mode=participate なら空席を1つ確保する。
func (m *Manager) Join(r *Room, sess *Session, name, mode string) (*Member, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if len([]rune(name)) > maxUserNameLen {
		return nil, ErrNameTooLong
	}
	m.mu.Lock()
	sess.Name = name
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if member, ok := r.Members[sess.UserID]; ok {
		member.Name = name
		return member, nil
	}
	member := &Member{UserID: sess.UserID, Name: name, IsHost: false, Joined: time.Now()}
	r.Members[sess.UserID] = member
	if mode == "participate" {
		if r.Status != StatusWaiting {
			return nil, ErrAlreadyStarted
		}
		seat := r.freeSeatLocked()
		if seat == nil {
			return nil, ErrRoomFull
		}
		seat.UserID = sess.UserID
		seat.UserName = name
		member.SeatID = seat.ID
	}
	m.broadcastLocked(r)
	return member, nil
}

// freeSeatLocked は r.mu 保持中に呼ぶ。
func (r *Room) freeSeatLocked() *Seat {
	for _, s := range r.Seats {
		if s.UserID == "" && !s.Connected {
			return s
		}
	}
	return nil
}

// AgentJoin は seat_token でのAI接続を席に結びつける。conn に席情報を設定するためポインタで受ける。
func (m *Manager) AgentJoin(roomID, token string, conn *model.Connection) error {
	m.mu.Lock()
	r := m.rooms[roomID]
	if r == nil {
		m.mu.Unlock()
		return ErrRoomNotFound
	}
	seatAny, ok := m.seatTokens.Load(token)
	m.mu.Unlock()
	if !ok {
		return ErrInvalidToken
	}
	seat := seatAny.(*Seat)

	r.mu.Lock()
	defer r.mu.Unlock()
	if !containsSeat(r.Seats, seat) {
		return ErrInvalidToken
	}
	if r.Status != StatusWaiting {
		return ErrAlreadyStarted
	}
	if seat.Connected {
		slog.Warn("席への再接続を受け付けました", "room", roomID, "seat", seat.ID)
		if old, ok := m.seatConns.Load(seat); ok {
			old.(*model.Connection).Conn.Close()
		}
	}
	seat.Connected = true
	seat.Team = conn.TeamName
	seat.Original = conn.OriginalName
	conn.Seat = &model.SeatContext{RoomID: roomID, SeatID: seat.ID, KeyPhrase: seat.KeyPhrase, Inbox: seat.Inbox}
	m.seatConns.Store(seat, conn)
	go m.watchSeat(r, seat, conn)
	m.broadcastLocked(r)
	slog.Info("AIが席に接続しました", "room", roomID, "seat", seat.ID, "team", conn.TeamName)
	return nil
}

func (m *Manager) watchSeat(r *Room, seat *Seat, conn *model.Connection) {
	<-conn.Done()
	r.mu.Lock()
	defer r.mu.Unlock()
	// 新しい接続に交代済み、またはゲーム開始済みなら席の状態を上書きしない。
	current, ok := m.seatConns.Load(seat)
	if r.Status != StatusWaiting || !ok || current != conn {
		return
	}
	m.seatConns.Delete(seat)
	seat.Connected = false
	seat.Team = ""
	seat.Original = ""
	m.broadcastLocked(r)
	slog.Info("待機中のAIが切断しました", "room", r.ID, "seat", seat.ID)
}

func containsSeat(seats []*Seat, target *Seat) bool {
	for _, s := range seats {
		if s == target {
			return true
		}
	}
	return false
}

// Start はホスト操作でゲームを開始する。
func (m *Manager) Start(r *Room, sess *Session) error {
	r.mu.Lock()
	if r.Status != StatusWaiting {
		r.mu.Unlock()
		return ErrAlreadyStarted
	}
	if sess == nil || sess.UserID != r.HostID {
		r.mu.Unlock()
		return ErrNotHost
	}
	if r.connectedCount() != r.AgentCnt {
		r.mu.Unlock()
		return ErrNotReady
	}
	r.Status = StatusStarting
	r.mu.Unlock()

	m.startGame(r)
	return nil
}

// startGame は席の接続からゲームを生成して非同期で進行する。
func (m *Manager) startGame(r *Room) {
	r.mu.Lock()
	conns := make([]model.Connection, 0, len(r.Seats))
	for _, s := range r.sortedSeats() {
		if v, ok := m.seatConns.Load(s); ok {
			conns = append(conns, *v.(*model.Connection))
		}
	}
	if len(conns) != r.AgentCnt {
		r.Status = StatusWaiting
		r.mu.Unlock()
		m.broadcast(r)
		return
	}
	game := logic.NewGame(&r.Config, r.settings, conns)
	r.game = game
	r.gameID = game.GetID()
	r.Status = StatusRunning
	// 席順とagent idxを一致させる（CreateAgentsはconn順にidxを振る）。
	views := game.AgentViews()
	for i, s := range r.sortedSeats() {
		s.AgentIdx = i + 1
		if i < len(views) {
			s.Role = views[i].Role.String()
			s.GameName = views[i].GameName
		}
	}
	r.mu.Unlock()
	m.broadcast(r)
	slog.Info("ルームのゲームを開始します", "room", r.ID, "game", game.GetID())

	// このゲーム専用の記録者を合成して、roomイベントを拾う。
	recorder := &gameRecorder{manager: m, room: r}
	game.SetObserver(observer.NewComposite(recorder, m.newObserve()))

	go func() {
		win := game.Start()
		r.mu.Lock()
		if win == model.T_NONE {
			r.Status = StatusAborted
			r.AbortMsg = "勝敗が決まる前に終了しました"
		} else {
			r.Status = StatusFinished
			r.WinSide = string(win)
		}
		r.mu.Unlock()
		m.broadcast(r)
		m.closeSubscribers(r)
		slog.Info("ルームのゲームが終了しました", "room", r.ID, "win", win)
	}()
}

// Claim はキーフレーズを検証して所有者の視点を解放する。
func (m *Manager) Claim(r *Room, sess *Session, phrase string) error {
	if sess == nil {
		return ErrNotAllowed
	}
	phrase = strings.TrimSpace(phrase)
	r.mu.Lock()
	defer r.mu.Unlock()
	seat := r.seatByUser(sess.UserID)
	if seat == nil {
		return ErrSeatNotClaimable
	}
	if r.Status != StatusRunning || seat.KeyPhrase == "" {
		return ErrSeatNotClaimable
	}
	if seat.KeyPhrase != phrase {
		return ErrInvalidPhrase
	}
	seat.Claimed = true
	m.broadcastLocked(r)
	slog.Info("視点が解放されました", "room", r.ID, "user", sess.UserID)
	return nil
}

// Leave は待機中の席を解放する。開始後の退出は扱わない。
func (m *Manager) Leave(r *Room, sess *Session) error {
	if sess == nil {
		return ErrNotAllowed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Status != StatusWaiting {
		return ErrAlreadyStarted
	}
	member, ok := r.Members[sess.UserID]
	if !ok {
		return nil
	}
	if member.IsHost {
		// ホスト退出は部屋を閉じる扱いにする。
		r.Status = StatusClosed
		for _, seat := range r.Seats {
			if old, ok := m.seatConns.LoadAndDelete(seat); ok {
				old.(*model.Connection).Conn.Close()
			}
		}
		m.closeSubscribersLocked(r)
	} else if seat := r.seatByUser(sess.UserID); seat != nil {
		if old, ok := m.seatConns.LoadAndDelete(seat); ok {
			old.(*model.Connection).Conn.Close()
		}
		seat.UserID = ""
		seat.UserName = ""
		seat.Connected = false
		seat.Team = ""
		seat.Original = ""
		member.SeatID = ""
	}
	delete(r.Members, sess.UserID)
	m.broadcastLocked(r)
	return nil
}

// Close はホストが待機中の部屋を閉じる。
func (m *Manager) Close(r *Room, sess *Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sess == nil || sess.UserID != r.HostID {
		return ErrNotHost
	}
	if r.Status != StatusWaiting {
		return ErrAlreadyStarted
	}
	r.Status = StatusClosed
	for _, seat := range r.Seats {
		if old, ok := m.seatConns.LoadAndDelete(seat); ok {
			old.(*model.Connection).Conn.Close()
		}
	}
	m.broadcastLocked(r)
	m.closeSubscribersLocked(r)
	return nil
}

// SendAdvice は所有者からAIへの助言を受信箱へ詰める。死亡済み・未解放は拒否する。
func (m *Manager) SendAdvice(r *Room, sess *Session, text string) error {
	if sess == nil {
		return ErrNotAllowed
	}
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > maxConsultLen {
		return ErrInvalidInput
	}
	r.mu.Lock()
	seat := r.seatByUser(sess.UserID)
	if seat == nil || !seat.Claimed || r.Status != StatusRunning || !seat.Alive {
		r.mu.Unlock()
		return ErrNotAllowed
	}
	seat.Inbox.Push(text)
	idx := seat.AgentIdx
	r.appendEvent(&Event{Type: "owner_advice", Day: r.day, FromIdx: &idx, Text: text, Private: true, OnlyIdx: []int{idx}})
	e := r.Events[len(r.Events)-1]
	m.broadcastEventLocked(r, e)
	r.mu.Unlock()
	return nil
}

// History は閲覧者に許可されたイベントをcursor以降で返す。
func (m *Manager) History(r *Room, sess *Session, cursor int) ([]*Event, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*Event{}
	for _, e := range r.Events {
		if e.Seq <= cursor {
			continue
		}
		if r.canSee(e, sess) {
			out = append(out, e)
		}
	}
	return out, r.seq
}

// ---- SSE 購読 ----

// Subscribe は閲覧者の購読を登録する。返り値のチャネルは閉室・終了時にcloseされる。
func (m *Manager) Subscribe(r *Room, sess *Session) (<-chan json.RawMessage, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSub++
	sub := &subscriber{ch: make(chan json.RawMessage, 64), session: sess}
	r.subs[r.nextSub] = sub
	id := r.nextSub
	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if s, ok := r.subs[id]; ok {
			delete(r.subs, id)
			close(s.ch)
		}
	}
	return sub.ch, cancel, true
}

// broadcastLocked はルーム全体の状態変化を通知する（詳細は GET で再取得）。r.mu 保持中に呼ぶ。
func (m *Manager) broadcastLocked(r *Room) {
	payload, _ := json.Marshal(map[string]any{
		"type": "room",
		"room": projectSummary(r),
		"seq":  r.seq,
	})
	for _, s := range r.subs {
		select {
		case s.ch <- payload:
		default:
		}
	}
}

func (m *Manager) broadcast(r *Room) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m.broadcastLocked(r)
}

// broadcastEventLocked は新規イベントを購読者へ流す。閲覧権限を購読者ごとに判定する。r.mu 保持中に呼ぶ。
func (m *Manager) broadcastEventLocked(r *Room, e *Event) {
	for _, s := range r.subs {
		if !r.canSee(e, s.session) {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"type": "event", "event": e, "seq": r.seq})
		select {
		case s.ch <- payload:
		default:
			// 満杯なら捨てる。クライアントはseqで欠落を検出してhistory再取得する。
		}
	}
}

// projectSummary は閲覧者に依存しない軽量の部屋サマリ。r.mu 保持中に呼ぶ。
func projectSummary(r *Room) map[string]any {
	return map[string]any{
		"room_id":   r.ID,
		"status":    string(r.Status),
		"connected": r.connectedCount(),
		"day":       r.day,
		"win_side":  r.WinSide,
	}
}

func (m *Manager) closeSubscribers(r *Room) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m.closeSubscribersLocked(r)
}

// closeSubscribersLocked は r.mu 保持中に呼ぶ。
func (m *Manager) closeSubscribersLocked(r *Room) {
	for id, s := range r.subs {
		delete(r.subs, id)
		close(s.ch)
	}
}
