package room

import (
	"testing"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
)

type batchRentalDriver struct {
	max   int
	seats map[*Seat]RentalAgent
}

func (d *batchRentalDriver) Available() (bool, string) {
	return len(d.seats) < d.max, ""
}

func (d *batchRentalDriver) AvailableSlots() int {
	return d.max - len(d.seats)
}

func (d *batchRentalDriver) Attach(_ *Room, seat *Seat, name, skill string, _ func(string, string)) error {
	d.seats[seat] = RentalAgent{Name: name, Skill: skill}
	return nil
}

func (d *batchRentalDriver) Detach(seat *Seat) {
	delete(d.seats, seat)
}

func newBatchRentalManager(t *testing.T) (*Manager, *batchRentalDriver) {
	t.Helper()
	cfg, err := model.LoadFromPath("../config/default_5.yml")
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(*cfg, nil)
	d := &batchRentalDriver{max: 5, seats: map[*Seat]RentalAgent{}}
	m.SetRentalDriver(d)
	return m, d
}

func TestCreateRoomWithMultipleRentalAgentsKeepsOnePrivateView(t *testing.T) {
	m, driver := newBatchRentalManager(t)
	host, _ := m.GetOrCreateSession("")
	r, err := m.CreateRoom(host, CreateParams{
		UserName: "主催者", AgentCount: 5, Mode: "participate", AgentSource: "rental",
		RentalAgents: []RentalAgent{{Name: "シオン", Skill: "慎重に推理"}, {Name: "ミオ", Skill: "積極的に質問"}, {Name: "ユキ", Skill: "論点を整理"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.seats) != 3 || len(r.Seats) != 5 || r.Members[host.UserID].SeatID != r.Seats[0].ID {
		t.Fatalf("複数席の確保に失敗しました: workers=%d seats=%d", len(driver.seats), len(r.Seats))
	}
	if r.freeSeatLocked() != r.Seats[3] {
		t.Fatal("管理中のレンタル席が空席として扱われました")
	}
	friend, _ := m.GetOrCreateSession("")
	if _, err := m.Join(r, friend, "友達", "participate", "external", "", ""); err != nil {
		t.Fatal(err)
	}
	if r.Seats[1].UserID != "" || r.Seats[1].ManagedBy != host.UserID || r.Members[friend.UserID].SeatID != r.Seats[3].ID {
		t.Fatal("友達がレンタル席を取得してしまいました")
	}
	if err := m.UpdateRental(r, friend, r.Seats[1].ID, "侵入", "変更"); err != ErrSeatNotClaimable {
		t.Fatalf("他人が追加席を変更できました: %v", err)
	}
	if err := m.UpdateRental(r, host, r.Seats[1].ID, "ミオ改", "新しい方針"); err != nil {
		t.Fatal(err)
	}
	if driver.seats[r.Seats[1]].Name != "ミオ改" {
		t.Fatal("追加席の設定がワーカーに反映されませんでした")
	}

	r.mu.Lock()
	r.Status = StatusRunning
	r.Seats[0].AgentIdx, r.Seats[0].Role = 1, "VILLAGER"
	r.Seats[1].AgentIdx, r.Seats[1].Role = 2, "SEER"
	r.Seats[1].RentalError = "非公開の失敗理由"
	r.Seats[1].RentalState = "degraded"
	r.mu.Unlock()
	projected := r.Project(host)
	seats := projected["seats"].([]map[string]any)
	var mine, managed map[string]any
	for _, seat := range seats {
		if seat["seat_id"] == r.Seats[0].ID {
			mine = seat
		}
		if seat["seat_id"] == r.Seats[1].ID {
			managed = seat
		}
	}
	if mine["role"] != "VILLAGER" || managed["role"] != "非公開" || managed["rental_state"] != "playing" {
		t.Fatalf("役職または状態が漏れました: mine=%v managed=%v", mine, managed)
	}
	for _, key := range []string{"key_phrase", "rental_skill", "rental_error"} {
		if _, ok := managed[key]; ok {
			t.Fatalf("追加席の%sが公開されました", key)
		}
	}
	if view := r.PrivateView(host); view["agent_idx"] != 1 || view["role"] != "VILLAGER" {
		t.Fatalf("作成者の視点が先頭の席ではありません: %v", view)
	}
	if r.canSee(&Event{OnlyIdx: []int{2}}, host) {
		t.Fatal("追加席の個別イベントが作成者に見えました")
	}
	if err := m.UpdateRental(r, host, r.Seats[1].ID, "ミオ改", "変更"); err != ErrAlreadyStarted {
		t.Fatalf("開始後に設定を変更できました: %v", err)
	}
}

func TestCreateRoomRejectsBatchBeyondAvailableCapacity(t *testing.T) {
	m, driver := newBatchRentalManager(t)
	first, _ := m.GetOrCreateSession("")
	_, err := m.CreateRoom(first, CreateParams{UserName: "一人目", AgentCount: 5, Mode: "participate", AgentSource: "rental", RentalAgents: []RentalAgent{{Name: "A"}, {Name: "B"}, {Name: "C"}}})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := m.GetOrCreateSession("")
	if _, err := m.CreateRoom(second, CreateParams{UserName: "二人目", AgentCount: 5, Mode: "participate", AgentSource: "rental", RentalAgents: []RentalAgent{{Name: "D"}, {Name: "E"}, {Name: "F"}}}); err == nil {
		t.Fatal("空き枠を超える一括作成を受け付けました")
	}
	if len(driver.seats) != 3 || len(m.rooms) != 1 {
		t.Fatalf("拒否後に席または部屋が増えました: workers=%d rooms=%d", len(driver.seats), len(m.rooms))
	}
	if cap := m.RentalCapabilities()["available_slots"]; cap != 2 {
		t.Fatalf("残り枠が正しくありません: %v", cap)
	}
}
