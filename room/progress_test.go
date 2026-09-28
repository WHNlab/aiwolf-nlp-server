package room

import (
	"encoding/json"
	"testing"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
)

func TestPublicProgressProjectsSequentialTurnAndNotifiesWithoutHistory(t *testing.T) {
	m := &Manager{}
	r := &Room{
		ID: "room-1", Status: StatusRunning, day: 2,
		phase: model.PublicPhaseDayDiscussion, subs: map[int]*subscriber{},
	}
	ch := make(chan json.RawMessage, 4)
	r.subs[1] = &subscriber{ch: ch}
	recorder := &gameRecorder{manager: m, room: r}

	recorder.OnPublicTurnStart("game-1", model.PublicTurnView{TurnID: "day-2-turn-4", AgentIdx: 3})
	if r.seq != 0 {
		t.Fatalf("progress update changed event sequence: got %d", r.seq)
	}
	var notice map[string]any
	if err := json.Unmarshal(<-ch, &notice); err != nil {
		t.Fatal(err)
	}
	if notice["type"] != "room" || notice["seq"] != float64(0) {
		t.Fatalf("unexpected progress notice: %#v", notice)
	}
	progress := notice["room"].(map[string]any)["progress"].(map[string]any)
	active := progress["active_public_turn"].(map[string]any)
	if progress["phase"] != string(model.PublicPhaseDayDiscussion) || active["turn_id"] != "day-2-turn-4" || active["agent_idx"] != float64(3) || active["state"] != "waiting" || active["deadline_at"] != nil {
		t.Fatalf("unexpected active public turn projection: %#v", progress)
	}

	r.Events = append(r.Events, &Event{Seq: 1, Type: "talk", FromIdx: intPtr(1), Text: "accepted public talk"})
	r.seq = 1
	recorder.OnPublicTurnEnd("game-1", "day-2-turn-4")
	if r.Events[0].Text != "accepted public talk" || r.seq != 1 {
		t.Fatalf("turn completion changed accepted talk history: events=%#v seq=%d", r.Events, r.seq)
	}
	<-ch
	projected := r.Project(nil)["progress"].(map[string]any)
	if projected["active_public_turn"] != (*publicTurnProgress)(nil) {
		t.Fatalf("active turn was not cleared: %#v", projected)
	}
}

func TestPublicProgressHidesNightActorsAndClearsTurnAtPhaseEnd(t *testing.T) {
	m := &Manager{}
	r := &Room{ID: "room-2", Status: StatusRunning, phase: model.PublicPhaseDayDiscussion, subs: map[int]*subscriber{}}
	recorder := &gameRecorder{manager: m, room: r}
	recorder.OnPublicTurnStart("game-2", model.PublicTurnView{TurnID: "public", AgentIdx: 2})
	if r.activePublicTurn == nil {
		t.Fatal("public TALK turn was not projected")
	}
	recorder.OnPublicPhase("game-2", model.PublicPhaseNight, 3)
	if r.phase != model.PublicPhaseNight || r.activePublicTurn != nil {
		t.Fatalf("night transition did not clear public turn: phase=%q active=%#v", r.phase, r.activePublicTurn)
	}
	revision := r.progressRevision
	recorder.OnPublicTurnStart("game-2", model.PublicTurnView{TurnID: "secret-night-actor", AgentIdx: 5})
	if r.activePublicTurn != nil || r.progressRevision != revision {
		t.Fatalf("night actor leaked into public progress: active=%#v revision=%d", r.activePublicTurn, r.progressRevision)
	}
	recorder.OnPublicPhase("game-2", model.PublicPhaseFinished, 3)
	projected := r.Project(nil)["progress"].(map[string]any)
	if projected["phase"] != string(model.PublicPhaseFinished) || projected["active_public_turn"] != (*publicTurnProgress)(nil) {
		t.Fatalf("unexpected finished progress: %#v", projected)
	}
}

func TestGameEndClearsPendingTurnAndPublishesFinishedProgress(t *testing.T) {
	m := &Manager{}
	r := &Room{
		ID: "room-end", Status: StatusRunning, day: 4,
		phase:            model.PublicPhaseDayDiscussion,
		activePublicTurn: &publicTurnProgress{TurnID: "pending", AgentIdx: 1, State: "waiting"},
		subs:             map[int]*subscriber{},
	}
	recorder := &gameRecorder{manager: m, room: r}
	recorder.OnGameEnd("game-end", model.T_NONE, model.GameState{Day: 4})
	progress := r.Project(nil)["progress"].(map[string]any)
	if progress["phase"] != string(model.PublicPhaseFinished) || progress["active_public_turn"] != (*publicTurnProgress)(nil) {
		t.Fatalf("game end left pending public progress: %#v", progress)
	}
}

func TestWaitingProgressHasExplicitNullActiveTurn(t *testing.T) {
	r := &Room{ID: "room-3", Status: StatusWaiting, phase: model.PublicPhaseWaiting}
	data, err := json.Marshal(r.Project(nil)["progress"])
	if err != nil {
		t.Fatal(err)
	}
	var progress map[string]any
	if err := json.Unmarshal(data, &progress); err != nil {
		t.Fatal(err)
	}
	if progress["phase"] != "waiting" || progress["revision"] != float64(0) || progress["active_public_turn"] != nil {
		t.Fatalf("unexpected waiting progress: %s", data)
	}
}

func TestHistoryFullRefetchIncludesVoteAfterNextDayDisclosure(t *testing.T) {
	m := &Manager{}
	e := &Event{Seq: 1, Type: "vote", Day: 1, RevealNext: true}
	r := &Room{day: 1, Events: []*Event{e}, seq: 1}
	if history, _ := m.History(r, nil, 0); len(history) != 0 {
		t.Fatalf("vote was visible before the following day: %#v", history)
	}
	r.mu.Lock()
	r.day = 2
	r.mu.Unlock()
	history, cursor := m.History(r, nil, 0)
	if len(history) != 1 || history[0] != e || cursor != 1 {
		t.Fatalf("full refetch did not reveal the previous-day vote: history=%#v cursor=%d", history, cursor)
	}
}

func TestConfirmedDeathImmediatelyChangesOwnerPerspective(t *testing.T) {
	for _, kind := range []string{"execute", "attack", "guarded"} {
		t.Run(kind, func(t *testing.T) {
			owner := &Session{UserID: "owner"}
			seat := &Seat{ID: "seat-1", UserID: "owner", AgentIdx: 1, Claimed: true, Alive: true, Role: "VILLAGER"}
			r := &Room{Status: StatusRunning, Seats: []*Seat{seat}, subs: map[int]*subscriber{}}
			recorder := &gameRecorder{manager: &Manager{}, room: r}
			agent := &model.AgentView{Idx: 1, Role: model.R_VILLAGER}
			if kind == "execute" {
				recorder.OnExecute("game", 2, agent, model.GameState{})
			} else {
				recorder.OnAttack("game", 2, agent, kind == "guarded", model.GameState{})
			}
			viewer := r.Project(owner)["viewer"].(map[string]any)
			if kind == "guarded" {
				if viewer["view_mode"] != string(ViewAgent) || viewer["can_consult"] != true {
					t.Fatalf("guarded player treated as dead: %#v", viewer)
				}
			} else if viewer["view_mode"] != string(ViewOmniscient) || viewer["can_consult"] != false {
				t.Fatalf("death left stale owner permissions: %#v", viewer)
			}
			if r.Project(nil)["viewer"].(map[string]any)["view_mode"] != string(ViewPublic) {
				t.Fatal("another player's death exposed the omniscient view")
			}
		})
	}
}
