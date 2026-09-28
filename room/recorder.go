package room

import (
	"log/slog"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/aiwolfdial/aiwolf-nlp-server/observer"
)

// gameRecorder はobserver経由でゲームイベントをルームのEvent履歴へ記録する。
// logicへルーム概念を持ち込まないため、observerとして横から記録する。
type gameRecorder struct {
	observer.NoopObserver
	manager *Manager
	room    *Room
}

// append はルームのイベント履歴への追加と購読者への配信をまとめる。
func (g *gameRecorder) append(e *Event) {
	g.room.mu.Lock()
	defer g.room.mu.Unlock()
	g.room.appendEvent(e)
	g.manager.broadcastEventLocked(g.room, e)
}

func (g *gameRecorder) OnGameStart(id string, agents []model.AgentView, state model.GameState) {
	g.append(&Event{Type: "game_start", Day: state.Day, Text: "ゲームが開始されました"})
}

func (g *gameRecorder) OnGameEnd(id string, winSide model.Team, state model.GameState) {
	g.room.mu.Lock()
	if g.room.phase != model.PublicPhaseFinished || g.room.activePublicTurn != nil || g.room.day != state.Day {
		g.room.phase = model.PublicPhaseFinished
		g.room.day = state.Day
		g.room.activePublicTurn = nil
		g.room.progressRevision++
		g.manager.broadcastLocked(g.room)
	}
	g.room.mu.Unlock()
	g.append(&Event{Type: "game_end", Day: state.Day, Text: "ゲームが終了しました", Team: string(winSide)})
}

func (g *gameRecorder) OnPublicPhase(id string, phase model.PublicPhase, day int) {
	switch phase {
	case model.PublicPhaseDayDiscussion, model.PublicPhaseDayVote, model.PublicPhaseNight, model.PublicPhaseFinished:
	default:
		return
	}
	g.room.mu.Lock()
	defer g.room.mu.Unlock()
	changed := g.room.phase != phase || g.room.day != day || g.room.activePublicTurn != nil
	if !changed {
		return
	}
	g.room.phase = phase
	g.room.day = day
	g.room.activePublicTurn = nil
	g.room.progressRevision++
	g.manager.broadcastLocked(g.room)
}

func (g *gameRecorder) OnPublicTurnStart(id string, turn model.PublicTurnView) {
	g.room.mu.Lock()
	defer g.room.mu.Unlock()
	if g.room.phase != model.PublicPhaseDayDiscussion || turn.TurnID == "" || turn.AgentIdx <= 0 {
		return
	}
	g.room.activePublicTurn = &publicTurnProgress{
		TurnID: turn.TurnID, AgentIdx: turn.AgentIdx, State: "waiting",
	}
	g.room.progressRevision++
	g.manager.broadcastLocked(g.room)
}

func (g *gameRecorder) OnPublicTurnEnd(id string, turnID string) {
	g.room.mu.Lock()
	defer g.room.mu.Unlock()
	if g.room.activePublicTurn == nil || g.room.activePublicTurn.TurnID != turnID {
		return
	}
	g.room.activePublicTurn = nil
	g.room.progressRevision++
	g.manager.broadcastLocked(g.room)
}

// OnDayStatus は日付境界と生死の確定を記録する。
// 死亡した席の所有者は即座に神視点へ切り替わる（投影側で判定）。
func (g *gameRecorder) OnDayStatus(id string, day int, statuses []model.AgentStatus) {
	g.room.mu.Lock()
	defer g.room.mu.Unlock()
	progressChanged := g.room.day != day
	g.room.day = day
	if progressChanged {
		g.room.progressRevision++
	}
	changed := false
	for _, st := range statuses {
		seat := g.room.seatByIdx(st.Idx)
		if seat == nil {
			continue
		}
		alive := st.Status == string(model.S_ALIVE)
		if seat.Alive && !alive {
			changed = true
		}
		seat.Alive = alive
	}
	g.room.appendEvent(&Event{Type: "day", Day: day})
	g.manager.broadcastEventLocked(g.room, g.room.Events[len(g.room.Events)-1])
	if changed || progressChanged {
		g.manager.broadcastLocked(g.room)
	}
}

func (g *gameRecorder) OnResult(id string, day int, villagers int, werewolves int, winSide model.Team) {
	g.append(&Event{Type: "result", Day: day, Team: string(winSide)})
}

func (g *gameRecorder) OnTalk(id string, day int, request model.Request, talk model.TalkView, voiceID *int, state model.GameState) {
	e := &Event{Type: "talk", Day: day, Text: talk.Text, FromIdx: intPtr(talk.Agent.Idx)}
	if request == model.R_WHISPER {
		e.Type = "whisper"
		e.RoleOnly = "WEREWOLF"
	}
	g.append(e)
}

func (g *gameRecorder) OnFreeformTalk(id string, agent model.AgentView, request model.Request, talk model.TalkView) {
	e := &Event{Type: "talk", Day: talk.Day, Text: talk.Text, FromIdx: intPtr(agent.Idx)}
	if request == model.R_WHISPER {
		e.Type = "whisper"
		e.RoleOnly = "WEREWOLF"
	}
	g.append(e)
}

func (g *gameRecorder) OnVote(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState) {
	e := &Event{Type: "vote", Day: day, FromIdx: intPtr(agent.Idx), ToIdx: intPtr(target.Idx)}
	// vote_visibility=true のとき翌日以降に公開。false のとき終了まで非公開。
	if g.room.Config.Game.VoteVisibility {
		e.RevealNext = true
	} else {
		e.OnlyIdx = allIdx(g.room)
	}
	g.append(e)
}

func (g *gameRecorder) OnAttackVote(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState) {
	g.append(&Event{Type: "attack_vote", Day: day, FromIdx: intPtr(agent.Idx), ToIdx: intPtr(target.Idx), RoleOnly: "WEREWOLF"})
}

func (g *gameRecorder) OnExecute(id string, day int, executed *model.AgentView, state model.GameState) {
	e := &Event{Type: "execute", Day: day}
	if executed != nil {
		e.ToIdx = intPtr(executed.Idx)
		e.Text = "追放されました"
		g.append(e)
		// 霊媒結果は霊媒師本人への通知情報。生存中はその席のみ、神視点・終了後は全員に見える。
		g.append(&Event{Type: "medium_result", Day: day, ToIdx: intPtr(executed.Idx), Result: roleSpecies(executed.Role), OnlyIdx: g.idxOfRole("MEDIUM")})
		return
	}
	e.Text = "追放者なし"
	g.append(e)
}

func (g *gameRecorder) OnDivine(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState) {
	g.append(&Event{Type: "divine_result", Day: day, FromIdx: intPtr(agent.Idx), ToIdx: intPtr(target.Idx), Result: string(target.Role.Species), OnlyIdx: []int{agent.Idx}})
}

func (g *gameRecorder) OnGuard(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState) {
	g.append(&Event{Type: "guard", Day: day, FromIdx: intPtr(agent.Idx), ToIdx: intPtr(target.Idx), OnlyIdx: []int{agent.Idx}})
}

func (g *gameRecorder) OnAttack(id string, day int, attacked *model.AgentView, guarded bool, state model.GameState) {
	e := &Event{Type: "attack", Day: day, Guarded: &guarded}
	if attacked != nil {
		e.ToIdx = intPtr(attacked.Idx)
		e.Text = "襲撃されました"
	} else {
		e.Text = "襲撃なし"
	}
	if guarded {
		e.Text = "襲撃は防がれました"
	}
	g.append(e)
}

func (g *gameRecorder) OnOwnerMessage(id string, day int, agent model.AgentView, message string) {
	g.append(&Event{Type: "owner_note", Day: day, FromIdx: intPtr(agent.Idx), Text: message, Private: true, OnlyIdx: []int{agent.Idx}})
}

func (g *gameRecorder) OnPhase(id string, request model.Request) {
	slog.Debug("フェーズ", "room", g.room.ID, "request", request.String())
}

func intPtr(v int) *int { return &v }

func allIdx(r *Room) []int {
	out := []int{}
	for _, s := range r.Seats {
		if s.AgentIdx > 0 {
			out = append(out, s.AgentIdx)
		}
	}
	return out
}

func (g *gameRecorder) idxOfRole(role string) []int {
	out := []int{}
	for _, s := range g.room.Seats {
		if s.Role == role {
			out = append(out, s.AgentIdx)
		}
	}
	return out
}

func roleSpecies(r model.Role) string {
	return string(r.Species)
}
