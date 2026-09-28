package room

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/oklog/ulid/v2"
)

const replayRetention = 30 * 24 * time.Hour

type replaySeat struct {
	ID        string `json:"id"`
	UserName  string `json:"user_name"`
	Team      string `json:"team"`
	GameName  string `json:"game_name"`
	AgentIdx  int    `json:"agent_idx"`
	Alive     bool   `json:"alive"`
	Role      string `json:"role"`
	Connected bool   `json:"connected"`
}

// replayFile にCookie・接続トークン・個別相談を含めない。
type replayFile struct {
	Version    int            `json:"version"`
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Public     bool           `json:"public"`
	AgentCnt   int            `json:"agent_count"`
	Status     Status         `json:"status"`
	WinSide    string         `json:"win_side"`
	AbortMsg   string         `json:"abort_message"`
	Day        int            `json:"day"`
	CreatedAt  time.Time      `json:"created_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Roles      map[string]int `json:"roles"`
	Seats      []replaySeat   `json:"seats"`
	Events     []Event        `json:"events"`
}

// InitArchive は既存の対戦記録を読み込み、保存先の問題を起動時に検出する。
func (m *Manager) InitArchive() error {
	dir := os.Getenv("AIWOLF_REPLAY_DIR")
	if dir == "" {
		dir = filepath.Join("data", "room-replays")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("対戦記録の保存先を作成できません: %w", err)
	}
	probe, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("対戦記録の保存先に書き込めません: %w", err)
	}
	probe.Close()
	os.Remove(probe.Name())
	m.archiveDir = dir
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("対戦記録を読み込めません: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		if _, err := ulid.ParseStrict(id); err != nil {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("対戦記録を読み込めませんでした", "path", path, "error", err)
			continue
		}
		var saved replayFile
		if err := json.Unmarshal(data, &saved); err != nil || saved.Version != 1 || saved.ID != id ||
			(saved.Status != StatusFinished && saved.Status != StatusAborted) || saved.FinishedAt.IsZero() {
			slog.Warn("不正な対戦記録を読み飛ばしました", "path", path)
			continue
		}
		if time.Since(saved.FinishedAt) >= replayRetention {
			if err := os.Remove(path); err != nil {
				slog.Warn("期限切れの対戦記録を削除できませんでした", "path", path, "error", err)
			}
			continue
		}
		r := &Room{
			ID: saved.ID, Name: saved.Name, Public: saved.Public, AgentCnt: saved.AgentCnt,
			Status: saved.Status, WinSide: saved.WinSide, AbortMsg: saved.AbortMsg,
			day: saved.Day, createdAt: saved.CreatedAt, finishedAt: saved.FinishedAt,
			archived: true, archivedRoles: saved.Roles, Members: map[string]*Member{},
			Events: make([]*Event, 0, len(saved.Events)), subs: map[int]*subscriber{},
			phase: model.PublicPhaseFinished,
		}
		for _, seat := range saved.Seats {
			r.Seats = append(r.Seats, &Seat{ID: seat.ID, UserName: seat.UserName, Team: seat.Team,
				GameName: seat.GameName, AgentIdx: seat.AgentIdx, Alive: seat.Alive,
				Role: seat.Role, Connected: seat.Connected})
		}
		for i := range saved.Events {
			e := saved.Events[i]
			r.Events = append(r.Events, &e)
			if e.Seq > r.seq {
				r.seq = e.Seq
			}
		}
		m.rooms[r.ID] = r
	}
	return nil
}

func (m *Manager) saveReplay(r *Room) error {
	if m.archiveDir == "" {
		return fmt.Errorf("対戦記録の保存先が初期化されていません")
	}
	r.mu.Lock()
	saved := replayFile{
		Version: 1, ID: r.ID, Name: r.Name, Public: r.Public,
		AgentCnt: r.AgentCnt, Status: r.Status, WinSide: r.WinSide, AbortMsg: r.AbortMsg,
		Day: r.day, CreatedAt: r.createdAt, FinishedAt: r.finishedAt,
		Roles: r.roleMap(), Seats: make([]replaySeat, 0, len(r.Seats)),
		Events: make([]Event, 0, len(r.Events)),
	}
	for _, s := range r.Seats {
		saved.Seats = append(saved.Seats, replaySeat{ID: s.ID, UserName: s.UserName,
			Team: s.Team, GameName: s.GameName, AgentIdx: s.AgentIdx,
			Alive: s.Alive, Role: s.Role, Connected: s.Connected})
	}
	for _, e := range r.Events {
		if r.canSee(e, nil) {
			saved.Events = append(saved.Events, *e)
		}
	}
	r.mu.Unlock()
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.archiveDir, ".replay-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(m.archiveDir, saved.ID+".json"))
}

func (m *Manager) pruneExpired() {
	m.mu.Lock()
	var expired []string
	for id, r := range m.rooms {
		r.mu.Lock()
		old := !r.finishedAt.IsZero() && time.Since(r.finishedAt) >= replayRetention
		r.mu.Unlock()
		if old {
			delete(m.rooms, id)
			expired = append(expired, id)
		}
	}
	m.mu.Unlock()
	for _, id := range expired {
		if m.archiveDir != "" {
			if err := os.Remove(filepath.Join(m.archiveDir, id+".json")); err != nil && !os.IsNotExist(err) {
				slog.Warn("期限切れの対戦記録を削除できませんでした", "room", id, "error", err)
			}
		}
	}
}
