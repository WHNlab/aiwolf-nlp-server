package room

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/aiwolfdial/aiwolf-nlp-server/logic"
	"github.com/aiwolfdial/aiwolf-nlp-server/model"
)

// Status は部屋のライフサイクル。
type Status string

const (
	StatusWaiting  Status = "waiting"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusFinished Status = "finished"
	StatusAborted  Status = "aborted"
	StatusClosed   Status = "closed"
)

// ViewMode はWeb閲覧者に見せる情報量。
type ViewMode string

const (
	ViewPublic     ViewMode = "public"
	ViewAgent      ViewMode = "agent"
	ViewOmniscient ViewMode = "omniscient"
)

var (
	ErrRoomNotFound     = errors.New("部屋が見つかりません")
	ErrInvalidInput     = errors.New("入力が不正です")
	ErrNameRequired     = errors.New("名前を入力してください")
	ErrNameTooLong      = errors.New("名前が長すぎます")
	ErrRoomNameTooLong  = errors.New("ルーム名が長すぎます")
	ErrRoomFull         = errors.New("参加枠が埋まっています")
	ErrAlreadyStarted   = errors.New("ゲームは既に開始されています")
	ErrNotReady         = errors.New("まだ開始できません")
	ErrNotHost          = errors.New("ホストのみ実行できます")
	ErrNotAllowed       = errors.New("権限がありません")
	ErrInvalidPhrase    = errors.New("キーフレーズを確認してください")
	ErrInvalidToken     = errors.New("招待トークンが無効です")
	ErrSeatNotClaimable = errors.New("この席は利用できません")
)

const (
	maxUserNameLen = 24
	maxRoomNameLen = 48
	maxConsultLen  = 1000
)

// Seat はAIが座る席。SeatContext経由で接続と結びつく。
type Seat struct {
	ID        string
	UserID    string // 席を予約した人間。観戦のみの部屋では空でもよい
	UserName  string
	Team      string // AIがNAME応答で名乗ったチーム名
	Original  string
	GameName  string // ゲーム開始後に割り当てられた表示名
	Token     string // AI接続用の秘密トークン
	KeyPhrase string // 開始時にAIへ渡し、所有者の視点解放に使うフレーズ
	Inbox     *model.OwnerInbox
	Connected bool
	Claimed   bool // 所有者がキーフレーズを検証済みか
	AgentIdx  int  // ゲーム開始後のidx。未割当は0
	Alive     bool
	Role      string // ゲーム開始後の役職（投影用）
}

// Member はルームへ入室した人間の記録。
type Member struct {
	UserID string
	Name   string
	IsHost bool
	SeatID string // 観戦者は空
	Joined time.Time
}

// Event はルームの時系列ログ。閲覧者ごとにフィルタして返す。
type Event struct {
	Seq        int       `json:"seq"`
	Type       string    `json:"type"`
	Day        int       `json:"day"`
	FromIdx    *int      `json:"from_idx,omitempty"`
	ToIdx      *int      `json:"to_idx,omitempty"`
	Text       string    `json:"text,omitempty"`
	Guarded    *bool     `json:"guarded,omitempty"`
	Result     string    `json:"result,omitempty"`
	Team       string    `json:"team,omitempty"`
	CreatedAt  time.Time `json:"at"`
	RevealNext bool      `json:"-"` // 翌日以降に公開（投票公開設定のとき）
	OnlyIdx    []int     `json:"-"` // 指定idxの席所有者のみ（生存中）
	RoleOnly   string    `json:"-"` // 指定役職の席所有者のみ（生存中）
	Private    bool      `json:"-"` // 終了後も公開しない（個別相談）
}

type subscriber struct {
	ch      chan json.RawMessage
	session *Session
}

type publicTurnProgress struct {
	TurnID     string     `json:"turn_id"`
	AgentIdx   int        `json:"agent_idx"`
	State      string     `json:"state"`
	DeadlineAt *time.Time `json:"deadline_at"`
}

// Room は1部屋1ゲームの状態を保持する。
// ルームの状態は r.mu、購読者一覧とセッションは Manager.mu が守る。
// ロック順は常に Manager.mu → Room.mu。
type Room struct {
	mu               sync.Mutex
	ID               string
	Name             string
	Config           model.Config
	AgentCnt         int
	Status           Status
	AbortMsg         string
	WinSide          string
	HostID           string
	Members          map[string]*Member
	Seats            []*Seat
	Events           []*Event
	seq              int
	day              int
	phase            model.PublicPhase
	progressRevision uint64
	activePublicTurn *publicTurnProgress
	gameID           string
	startedAt        time.Time
	createdAt        time.Time
	subs             map[int]*subscriber
	nextSub          int
	settings         *model.Setting
	game             *logic.Game
}

// Session はブラウザのCookieに対応する。UserIDはルーム横断で安定させる。
type Session struct {
	Token   string
	UserID  string
	Name    string
	Created time.Time
}

// sortedSeats は席順をID順に安定化する。呼び出し側が r.mu を持つ。
func (r *Room) sortedSeats() []*Seat {
	seats := make([]*Seat, len(r.Seats))
	copy(seats, r.Seats)
	sort.Slice(seats, func(i, j int) bool { return seats[i].ID < seats[j].ID })
	return seats
}

func (r *Room) seatByUser(userID string) *Seat {
	for _, s := range r.Seats {
		if s.UserID == userID {
			return s
		}
	}
	return nil
}

func (r *Room) seatByIdx(idx int) *Seat {
	for _, s := range r.Seats {
		if s.AgentIdx == idx {
			return s
		}
	}
	return nil
}

func (r *Room) connectedCount() int {
	n := 0
	for _, s := range r.Seats {
		if s.Connected {
			n++
		}
	}
	return n
}

// appendEvent は r.mu 保持中に呼ぶ。
func (r *Room) appendEvent(e *Event) {
	r.seq++
	e.Seq = r.seq
	e.CreatedAt = time.Now()
	r.Events = append(r.Events, e)
}

// viewModeOf は閲覧者の現在の視点を決める。r.mu 保持中に呼ぶ。
// 終了・中断後は全員が神視点（ゲーム内情報の解禁）。席所有者が死んでいれば神視点。
func (r *Room) viewModeOf(sess *Session) ViewMode {
	if r.Status == StatusFinished || r.Status == StatusAborted {
		return ViewOmniscient
	}
	if sess == nil {
		return ViewPublic
	}
	seat := r.seatByUser(sess.UserID)
	if seat == nil || !seat.Claimed {
		return ViewPublic
	}
	if r.Status == StatusRunning && !seat.Alive {
		return ViewOmniscient
	}
	return ViewAgent
}

// canSee はイベントをこの視点で見せられるか判定する。r.mu 保持中に呼ぶ。
func (r *Room) canSee(e *Event, sess *Session) bool {
	if e.Private {
		// 個別相談は当事者のみ。OnlyIdx に所有者の agent idx を入れている。
		if sess == nil {
			return false
		}
		seat := r.seatByUser(sess.UserID)
		if seat == nil {
			return false
		}
		return contains(e.OnlyIdx, seat.AgentIdx)
	}
	if r.viewModeOf(sess) == ViewOmniscient {
		return true
	}
	if len(e.OnlyIdx) > 0 {
		if sess == nil {
			return false
		}
		seat := r.seatByUser(sess.UserID)
		if seat == nil || !seat.Claimed || (r.Status == StatusRunning && !seat.Alive) {
			return false
		}
		return contains(e.OnlyIdx, seat.AgentIdx)
	}
	if e.RoleOnly != "" {
		if sess == nil {
			return false
		}
		seat := r.seatByUser(sess.UserID)
		if seat == nil || !seat.Claimed || seat.Role != e.RoleOnly || (r.Status == StatusRunning && !seat.Alive) {
			return false
		}
		return true
	}
	if e.RevealNext && r.day <= e.Day {
		return false
	}
	return true
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Project は閲覧者向けの部屋状態を組み立てる。
func (r *Room) Project(sess *Session) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	mode := r.viewModeOf(sess)
	isHost := sess != nil && sess.UserID == r.HostID
	claimable := false
	canConsult := false
	mySeatID := ""
	if sess != nil {
		if seat := r.seatByUser(sess.UserID); seat != nil {
			mySeatID = seat.ID
			claimable = r.Status == StatusRunning && !seat.Claimed && seat.KeyPhrase != ""
			canConsult = r.Status == StatusRunning && seat.Claimed && seat.Alive
		}
	}
	seats := make([]map[string]any, 0, len(r.Seats))
	for _, s := range r.sortedSeats() {
		entry := map[string]any{
			"seat_id":   s.ID,
			"user_name": s.UserName,
			"connected": s.Connected,
			"claimed":   s.Claimed,
			"alive":     s.Alive,
			"agent_idx": s.AgentIdx,
			"is_mine":   sess != nil && s.UserID == sess.UserID,
		}
		if s.Connected {
			entry["team_name"] = s.Team
			entry["game_name"] = s.GameName
		}
		if mode == ViewOmniscient || (sess != nil && s.UserID == sess.UserID && s.Claimed && s.Role != "") {
			entry["role"] = s.Role
		} else {
			entry["role"] = "非公開"
		}
		seats = append(seats, entry)
	}
	return map[string]any{
		"room_id":      r.ID,
		"name":         r.Name,
		"status":       string(r.Status),
		"agent_count":  r.AgentCnt,
		"connected":    r.connectedCount(),
		"day":          r.day,
		"win_side":     r.WinSide,
		"abort_reason": r.AbortMsg,
		"created_at":   r.createdAt,
		"roles":        r.roleMap(),
		"seats":        seats,
		"server_time":  time.Now(),
		"cursor":       r.seq,
		"progress": map[string]any{
			"phase":              string(r.phase),
			"revision":           r.progressRevision,
			"active_public_turn": r.activePublicTurn,
		},
		"viewer": r.viewerMap(sess, isHost, mode, claimable, canConsult, mySeatID),
	}
}

func (r *Room) viewerMap(sess *Session, isHost bool, mode ViewMode, claimable, canConsult bool, mySeatID string) map[string]any {
	v := map[string]any{
		"is_host":     isHost,
		"view_mode":   string(mode),
		"own_seat":    mySeatID,
		"joined":      sess != nil && r.Members[sess.UserID] != nil,
		"can_start":   isHost && r.Status == StatusWaiting && r.connectedCount() == r.AgentCnt,
		"can_claim":   claimable,
		"can_consult": canConsult,
		"user_id":     "",
		"user_name":   "",
	}
	if sess != nil {
		v["user_id"] = sess.UserID
		v["user_name"] = sess.Name
	}
	return v
}

func (r *Room) roleMap() map[string]int {
	out := make(map[string]int)
	if roles, ok := r.Config.Logic.Roles[r.AgentCnt]; ok {
		for k, v := range roles {
			out[k] = v
		}
	}
	return out
}

// seatGameName は席のゲーム内表示名を返す。r.mu 保持中に呼ぶ。
func (r *Room) seatGameName(idx int) string {
	if s := r.seatByIdx(idx); s != nil && s.GameName != "" {
		return s.GameName
	}
	return ""
}

// PrivateView は自分のAIが知っている情報（役職・占い・霊媒・仲間）。
func (r *Room) PrivateView(sess *Session) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sess == nil {
		return nil
	}
	seat := r.seatByUser(sess.UserID)
	if seat == nil || !seat.Claimed || r.Status == StatusWaiting || r.Status == StatusStarting {
		return nil
	}
	out := map[string]any{
		"agent_idx": seat.AgentIdx,
		"role":      seat.Role,
		"alive":     seat.Alive,
	}
	know := map[string]any{}
	role := model.RoleFromString(seat.Role)
	mode := r.viewModeOf(sess)
	if role == model.R_WEREWOLF || mode == ViewOmniscient {
		mates := []string{}
		for _, s := range r.sortedSeats() {
			if s.Role == "WEREWOLF" && s.AgentIdx != seat.AgentIdx {
				mates = append(mates, seatDisplayName(s))
			}
		}
		know["werewolf_mates"] = mates
	}
	if role == model.R_SEER || mode == ViewOmniscient {
		know["divine_results"] = r.judgeResults("divine_result", seat.AgentIdx)
	}
	if role == model.R_MEDIUM || mode == ViewOmniscient {
		know["medium_results"] = r.judgeResults("medium_result", seat.AgentIdx)
	}
	out["knowledge"] = know
	return out
}

func seatDisplayName(s *Seat) string {
	if s.GameName != "" {
		return s.GameName
	}
	if s.Team != "" {
		return s.Team
	}
	return s.UserName
}

// judgeResults は本人宛ての占い・霊媒結果を読みやすい形で返す。r.mu 保持中に呼ぶ。
func (r *Room) judgeResults(kind string, agentIdx int) []map[string]any {
	results := []map[string]any{}
	for _, e := range r.Events {
		if e.Type != kind || !contains(e.OnlyIdx, agentIdx) {
			continue
		}
		entry := map[string]any{"day": e.Day, "result": e.Result}
		if e.ToIdx != nil {
			entry["target"] = r.seatGameName(*e.ToIdx)
		}
		results = append(results, entry)
	}
	return results
}

// InviteFor は呼び出し者の席にAIを接続するための情報を返す。
// 席を持たない閲覧者には nil を返す。トークンは接続に必要な秘密情報。
func (r *Room) InviteFor(sess *Session, wsBase string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sess == nil {
		return nil
	}
	seat := r.seatByUser(sess.UserID)
	if seat == nil {
		return nil
	}
	url := wsBase + "?room_id=" + r.ID + "&seat_token=" + seat.Token
	mode := "turn"
	if r.Config.Game.Talk.Duration != nil || r.Config.Game.Whisper.Duration != nil {
		mode = "freeform"
	}
	config, _ := json.MarshalIndent(map[string]any{"ws_url": url, "room_id": r.ID, "mode": mode}, "", "  ")
	guide := "人狼参加キットを展開し、SKILL.mdの手順で参加してください。追加のLLM APIキーは不要です。\n" +
		"以下をinvite.jsonとして保存してください（この席の所有者とAIだけで扱う秘密情報です）。\n" + string(config) + "\n\n" +
		"CLIで接続し、ホストの開始を待ってください。開始後にinfo.key_phraseを私へ個別に伝えてください。\n" +
		"待機中は同じ--sessionでnext --wait 15を実行します。通知がなければ最大15秒でwaiting・空のeventsが返ります。その場合だけ5秒sleepして再実行し、最大3回（合計60秒）で打ち切って私に知らせてください。接続プロセスは切らず、LLMの操作を止めます。\n" +
		"私が再開を指示したら、同じ--sessionでresumeを1回実行して未読通知とpendingを確認してください。connectを再実行しないでください。action_requiredならpending.remaining_seconds以内にactし、expiredなら再送しないでください。\n" +
		"CLIのローカル制御から25秒応答がなければエラーとして止まり、私に知らせてください。\n" +
		"CLIがNAME応答と接続維持を行います。接続プログラムを自作する必要はありません。\n"
	if mode != "turn" {
		guide = "この部屋はグループチャット方式です。参加キット初版はターン制専用のため利用できません。対応するエージェントを接続してください。\n接続先: " + url
	}
	return map[string]any{
		"room_id":    r.ID,
		"seat_id":    seat.ID,
		"ws_url":     url,
		"mode":       mode,
		"seat_token": seat.Token,
		"guide_text": guide,
		"status":     string(r.Status),
	}
}
