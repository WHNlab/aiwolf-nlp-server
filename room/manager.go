package room

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

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
	rental     atomic.Value // RentalDriver。r.mu 内でも安全に読むためatomicにする
	rentalMu   sync.Mutex   // レンタル席の可否確認と確保を直列化する。常に最外側で取る。
	mu         sync.Mutex
	rooms      map[string]*Room
	archiveDir string
	sessions   map[string]*Session
	seatTokens sync.Map // トークン→*Seat。r.muの内側でも書き込めるようsync.Mapにする。
	seatConns  sync.Map // *Seat → *model.Connection（ゲーム開始時に束ねる）
}

// RentalDriver はレンタルAIの実行管理を抽象化する。
// room は呼び出しだけを担い、実装は rental パッケージが提供する。
type RentalDriver interface {
	// Available はレンタルが現在利用可能かを返す。理由はユーザー向けの簡潔な文言。
	Available() (bool, string)
	// Attach は席へ内部エージェントを接続する。失敗時はエラーを返す（接続の成否は
	// 非同期にonStateで報告されうる）。名前・スキルの変更は新しいworkerに適用される。
	Attach(r *Room, seat *Seat, name, skill string, onState func(state, msg string)) error
	// Detach は席に紐づくworkerを止める。未接続なら何もしない。
	Detach(seat *Seat)
}

// SetRentalDriver はレンタル実装を登録する。
func (m *Manager) SetRentalDriver(d RentalDriver) {
	m.rental.Store(d)
	slog.Info("レンタルAIドライバを登録しました")
}

func (m *Manager) rentalDriver() RentalDriver {
	if v := m.rental.Load(); v != nil {
		return v.(RentalDriver)
	}
	return nil
}

// RentalCapabilities はUI向けの利用可否を返す。
func (m *Manager) RentalCapabilities() map[string]any {
	d := m.rentalDriver()
	ok, reason := false, "レンタルAIは現在利用できません"
	if d != nil {
		ok, reason = d.Available()
	}
	if m.base.Game.Talk.Duration != nil || m.base.Game.Whisper.Duration != nil {
		ok, reason = false, "ターン制のルームのみレンタルAIに対応しています"
	}
	return map[string]any{
		"available":        ok,
		"reason":           reason,
		"max_skill_length": 200,
		"supported_counts": []int{5},
		"supported_modes":  []string{"turn"},
	}
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
	RoomName    string
	UserName    string
	AgentCount  int
	Mode        string // "participate" or "spectate"
	Public      bool
	AgentSource string // "external"（既定）または "rental"
	RentalName  string
	RentalSkill string
}

// normalizeRentalProfile は入力を検証し、補正済みの値を返す。UTF-8と制御文字を確認する。
func normalizeRentalProfile(name, skill string) (string, string, error) {
	name = strings.TrimSpace(name)
	skill = strings.TrimSpace(skill)
	if name == "" {
		name = "レンタルAI"
	}
	if len([]rune(name)) > maxAINameLen {
		return "", "", ErrNameTooLong
	}
	if len([]rune(skill)) > maxSkillLen {
		return "", "", ErrSkillTooLong
	}
	for _, s := range []string{name, skill} {
		if !utf8.ValidString(s) {
			return "", "", ErrInvalidInput
		}
		for _, r := range s {
			if r < 0x20 && r != '\n' && r != '\t' {
				return "", "", ErrInvalidInput
			}
		}
	}
	return name, skill, nil
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
		Public:    p.Public,
		Status:    StatusWaiting,
		phase:     model.PublicPhaseWaiting,
		HostID:    sess.UserID,
		Members:   map[string]*Member{},
		Events:    []*Event{},
		createdAt: time.Now(),
		subs:      map[int]*subscriber{},
		settings:  settings,
	}
	m.rooms[r.ID] = r
	m.mu.Unlock()

	// レンタル可否の確認はr.mu取得前に行う（m.muとのロック順を守るため）。
	// rentalMuは確認→席確保→Attachまで保持し、同時1席制限の競合を防ぐ。
	// r.Configとr.AgentCntは生成時に確定し以後変わらない。
	if p.AgentSource == "rental" {
		m.rentalMu.Lock()
		defer m.rentalMu.Unlock()
	}
	if p.Mode == "spectate" && p.AgentSource == "rental" {
		m.mu.Lock()
		delete(m.rooms, r.ID)
		m.mu.Unlock()
		return nil, ErrInvalidInput
	}
	switch p.AgentSource {
	case "", "external":
	case "rental":
		if _, _, err := normalizeRentalProfile(p.RentalName, p.RentalSkill); err != nil {
			m.mu.Lock()
			delete(m.rooms, r.ID)
			m.mu.Unlock()
			return nil, err
		}
		if err := m.checkRentalUsable(r, sess.UserID); err != nil {
			m.mu.Lock()
			delete(m.rooms, r.ID)
			m.mu.Unlock()
			return nil, err
		}
	default:
		m.mu.Lock()
		delete(m.rooms, r.ID)
		m.mu.Unlock()
		return nil, ErrInvalidInput
	}

	r.mu.Lock()
	member := &Member{UserID: sess.UserID, Name: p.UserName, IsHost: true, Joined: time.Now()}
	r.Members[sess.UserID] = member
	var rentalSeat *Seat
	switch {
	case p.Mode == "spectate":
	case p.AgentSource == "rental":
		name, skill, _ := normalizeRentalProfile(p.RentalName, p.RentalSkill)
		seat := m.addSeatLocked(r, member)
		seat.Source = "rental"
		seat.RentalName = name
		seat.RentalSkill = skill
		seat.RentalState = "preparing"
		seat.InternalToken = genToken(24)
		rentalSeat = seat
	case p.AgentSource == "" || p.AgentSource == "external":
		m.addSeatLocked(r, member)
	}
	m.addPlaceholderSeatsLocked(r)
	r.mu.Unlock()
	slog.Info("ルームを作成しました", "room", r.ID, "name", r.Name, "agents", r.AgentCnt)
	if rentalSeat != nil {
		m.attachRental(r, rentalSeat)
	}
	return r, nil
}

// addSeatLocked はメンバー用の席を一つ追加する。r.mu保持中に呼ぶ。
func (m *Manager) addSeatLocked(r *Room, member *Member) *Seat {
	seat := &Seat{
		ID:        "s" + ulid.Make().String(),
		UserID:    member.UserID,
		UserName:  member.Name,
		Token:     genToken(24),
		KeyPhrase: genToken(6), // 旧クライアント用の値を維持する。
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
	m.pruneExpired()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rooms[id]
}

// ListRooms は公開ルームのみを状態・名前・IDで探す。
func (m *Manager) ListRooms(status, query string) []map[string]any {
	m.pruneExpired()
	query = strings.ToLower(strings.TrimSpace(query))
	m.mu.Lock()
	rooms := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		rooms = append(rooms, r)
	}
	m.mu.Unlock()
	out := make([]map[string]any, 0)
	for _, r := range rooms {
		r.mu.Lock()
		active := r.Status == StatusWaiting || r.Status == StatusStarting || r.Status == StatusRunning
		finished := r.Status == StatusFinished || r.Status == StatusAborted
		if r.Public && ((status == "active" && active) || (status == "finished" && finished)) &&
			(query == "" || strings.Contains(strings.ToLower(r.Name), query) || strings.Contains(strings.ToLower(r.ID), query)) {
			out = append(out, map[string]any{
				"room_id": r.ID, "name": r.Name, "status": r.Status,
				"agent_count": r.AgentCnt, "connected": r.connectedCount(),
				"day": r.day, "created_at": r.createdAt, "finished_at": r.finishedAt,
			})
		}
		r.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		key := "created_at"
		if status == "finished" {
			key = "finished_at"
		}
		return out[i][key].(time.Time).After(out[j][key].(time.Time))
	})
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

// Join はユーザーを部屋へ入室させる。mode=participate なら空席を1つ確保する。
func (m *Manager) Join(r *Room, sess *Session, name, mode, agentSource, rentalName, rentalSkill string) (*Member, error) {
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

	// レンタル可否の確認はr.mu取得前に行う（ロック順 m.mu → r.mu を守るため）。
	// Config/AgentCntは作成後不変なのでロックなしで読める。既存メンバーの再入室では
	// ここを通っても席は再確保されないので問題ない。
	if agentSource == "rental" {
		m.rentalMu.Lock()
		defer m.rentalMu.Unlock()
		if mode != "participate" {
			return nil, ErrInvalidInput
		}
		var err error
		if rentalName, rentalSkill, err = normalizeRentalProfile(rentalName, rentalSkill); err != nil {
			return nil, err
		}
		if err := m.checkRentalUsable(r, sess.UserID); err != nil {
			return nil, err
		}
	} else if agentSource != "" && agentSource != "external" {
		return nil, ErrInvalidInput
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.archived || r.Status == StatusClosed || r.Status == StatusFinished || r.Status == StatusAborted {
		return nil, ErrAlreadyStarted
	}
	if member, ok := r.Members[sess.UserID]; ok {
		member.Name = name
		return member, nil
	}
	var available *Seat
	if mode == "participate" {
		if r.Status != StatusWaiting {
			return nil, ErrAlreadyStarted
		}
		available = r.freeSeatLocked()
		if available == nil {
			return nil, ErrRoomFull
		}
	}
	member := &Member{UserID: sess.UserID, Name: name, IsHost: false, Joined: time.Now()}
	r.Members[sess.UserID] = member
	if available != nil {
		switch agentSource {
		case "", "external":
		case "rental":
			// 検証・上限確認はr.mu取得前にrentalMu配下で済ませている。
			available.Source = "rental"
			available.RentalName = rentalName
			available.RentalSkill = rentalSkill
			available.RentalState = "preparing"
			available.InternalToken = genToken(24)
		default:
			delete(r.Members, sess.UserID)
			return nil, ErrInvalidInput
		}
		available.UserID = sess.UserID
		available.UserName = name
		member.SeatID = available.ID
	}
	m.broadcastLocked(r)
	// Attach はロック内で呼ぶが、接続自体はworkerのgoroutineが非同期で行う。
	if available != nil && available.Source == "rental" {
		if err := m.attachRental(r, available); err != nil {
			available.RentalState = "failed"
			available.RentalError = "レンタルAIを開始できませんでした"
			m.broadcastLocked(r)
		}
	}
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
	if seat.Source == "rental" {
		// レンタル席には内部ワーカー専用トークンでのみ接続する。seat_tokenを知っていても外部からは入れない。
		tok := conn.Header.Get("X-Rental-Token")
		if tok == "" || tok != seat.InternalToken {
			return ErrInvalidToken
		}
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
	if seat.Source == "rental" && r.Status == StatusWaiting {
		// 接続を張り替えた直後なら、古いワーカーの切断で席を壊さない。
		// InternalTokenはAttachごとに更新されるため、不一致は交代済みを意味する。
		tok := ""
		if conn.Header != nil {
			tok = conn.Header.Get("X-Rental-Token")
		}
		if tok == seat.InternalToken {
			seat.RentalState = "failed"
			seat.RentalError = "レンタルAIの接続が切れました"
			if d := m.rentalDriver(); d != nil {
				d.Detach(seat)
			}
		}
	}
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

// checkRentalUsable はルーム種別・全体可用性・ユーザー別上限を確認する。
// r.mu・m.mu を保持せず rentalMu 保持中に呼ぶ。
func (m *Manager) checkRentalUsable(r *Room, userID string) error {
	d := m.rentalDriver()
	if d == nil {
		return ErrRentalUnavailable
	}
	if ok, _ := d.Available(); !ok {
		return ErrRentalUnavailable
	}
	if r.Config.Game.Talk.Duration != nil || r.Config.Game.Whisper.Duration != nil || r.AgentCnt != 5 {
		return ErrInvalidInput
	}
	if m.countRentalSeats(userID, r.ID) >= maxRentalPerUser {
		return errors.New("レンタルAIの同時利用は1席までです")
	}
	return nil
}

// countRentalSeats は指定ユーザーの稼働中・待機中レンタル席数を返す。
func (m *Manager) countRentalSeats(userID, excludeRoomID string) int {
	m.mu.Lock()
	rooms := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		rooms = append(rooms, r)
	}
	m.mu.Unlock()
	n := 0
	for _, r := range rooms {
		if r.ID == excludeRoomID {
			continue
		}
		r.mu.Lock()
		active := r.Status == StatusWaiting || r.Status == StatusStarting || r.Status == StatusRunning
		if active {
			for _, s := range r.Seats {
				if s.Source == "rental" && s.UserID == userID {
					n++
				}
			}
		}
		r.mu.Unlock()
	}
	return n
}

// attachRental は席に内部workerを割り当てる。r.mu 保持中に呼ぶ。
func (m *Manager) attachRental(r *Room, seat *Seat) error {
	d := m.rentalDriver()
	if d == nil {
		return ErrRentalUnavailable
	}
	// 接続ごとに内部トークンを更新し、古いワーカーの切断・応答が
	// 新しいワーカーの席を壊さないようにする。
	seat.InternalToken = genToken(24)
	roomID := r.ID
	seatID := seat.ID
	return d.Attach(r, seat, seat.RentalName, seat.RentalSkill, func(state, msg string) {
		m.updateRentalState(roomID, seatID, state, msg)
	})
}

// updateRentalState はworkerからの状態通知を席へ反映する。
func (m *Manager) updateRentalState(roomID, seatID, state, msg string) {
	m.mu.Lock()
	r := m.rooms[roomID]
	m.mu.Unlock()
	if r == nil {
		return
	}
	r.mu.Lock()
	for _, s := range r.Seats {
		if s.ID == seatID {
			s.RentalState = state
			if msg != "" {
				s.RentalError = msg
			}
			if state == "ready" || state == "playing" {
				s.RentalError = ""
			}
			break
		}
	}
	m.broadcastLocked(r)
	r.mu.Unlock()
}

// UpdateRental は待機中のレンタル席の名前・SKILLを更新し、接続をやり直す。席所有者のみ。
func (m *Manager) UpdateRental(r *Room, sess *Session, name, skill string) error {
	if sess == nil {
		return ErrNotAllowed
	}
	name, skill, err := normalizeRentalProfile(name, skill)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Status != StatusWaiting {
		return ErrAlreadyStarted
	}
	seat := r.seatByUser(sess.UserID)
	if seat == nil || seat.Source != "rental" {
		return ErrSeatNotClaimable
	}
	seat.RentalName = name
	seat.RentalSkill = skill
	seat.RentalState = "preparing"
	seat.RentalError = ""
	err = m.attachRental(r, seat)
	if err != nil {
		seat.RentalState = "failed"
		seat.RentalError = "レンタルAIを開始できませんでした"
	}
	m.broadcastLocked(r)
	return err
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
		r.finishedAt = time.Now()
		r.mu.Unlock()
		if err := m.saveReplay(r); err != nil {
			slog.Error("対戦記録を保存できませんでした", "room", r.ID, "error", err)
		}
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
			if seat.Source == "rental" {
				if d := m.rentalDriver(); d != nil {
					d.Detach(seat)
				}
				seat.RentalState = "failed"
			}
		}
		m.closeSubscribersLocked(r)
	} else if seat := r.seatByUser(sess.UserID); seat != nil {
		if old, ok := m.seatConns.LoadAndDelete(seat); ok {
			old.(*model.Connection).Conn.Close()
		}
		if seat.Source == "rental" {
			if d := m.rentalDriver(); d != nil {
				d.Detach(seat)
			}
		}
		seat.UserID = ""
		seat.UserName = ""
		seat.Connected = false
		seat.Team = ""
		seat.Original = ""
		seat.Source = "external"
		seat.RentalName = ""
		seat.RentalSkill = ""
		seat.RentalState = ""
		seat.InternalToken = ""
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
		if seat.Source == "rental" {
			if d := m.rentalDriver(); d != nil {
				d.Detach(seat)
			}
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
	if seat == nil || r.Status != StatusRunning || !seat.Alive {
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
		"progress": map[string]any{
			"phase":              string(r.phase),
			"revision":           r.progressRevision,
			"active_public_turn": r.activePublicTurn,
		},
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
