package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
	"github.com/aiwolfdial/aiwolf-nlp-server/room"
)

type webBatchRentalDriver struct {
	seats map[*room.Seat]string
}

func (d *webBatchRentalDriver) Available() (bool, string) { return len(d.seats) < 5, "" }
func (d *webBatchRentalDriver) AvailableSlots() int       { return 5 - len(d.seats) }
func (d *webBatchRentalDriver) Attach(_ *room.Room, seat *room.Seat, name, _ string, _ func(string, string)) error {
	d.seats[seat] = name
	return nil
}
func (d *webBatchRentalDriver) Detach(seat *room.Seat) { delete(d.seats, seat) }

func TestRentalBatchCreateAndSeatUpdateAPI(t *testing.T) {
	cfg, err := model.LoadFromPath("../config/default_5.yml")
	if err != nil {
		t.Fatal(err)
	}
	m := room.NewManager(*cfg, nil)
	m.SetRentalDriver(&webBatchRentalDriver{seats: map[*room.Seat]string{}})
	s := &Server{config: *cfg, roomManager: m}
	router := s.buildWebRouter()
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	created := request(http.MethodPost, "/api/v1/rooms", `{"user_name":"主催者","agent_count":5,"mode":"participate","agent_source":"rental","rental_agents":[{"name":"シオン","skill":"慎重"},{"name":"ミオ","skill":"積極的"},{"name":"ユキ","skill":"整理する"}]}`, nil)
	if created.Code != http.StatusOK {
		t.Fatalf("一括作成に失敗しました: %d %s", created.Code, created.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	var managedID string
	for _, item := range data["seats"].([]any) {
		seat := item.(map[string]any)
		if seat["rental_name"] == "ミオ" && seat["is_managed"] == true {
			managedID = seat["seat_id"].(string)
		}
	}
	if managedID == "" {
		t.Fatalf("追加席が管理席として返されませんでした: %v", data["seats"])
	}
	cookie := created.Result().Cookies()[0]
	path := "/api/v1/rooms/" + data["room_id"].(string) + "/rentals/" + managedID
	unauthorized := request(http.MethodPut, path, `{"rental_name":"侵入","rental_skill":"変更"}`, nil)
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("他人が追加席を変更できました: %d", unauthorized.Code)
	}
	updated := request(http.MethodPut, path, `{"rental_name":"ミオ改","rental_skill":"新しい方針"}`, cookie)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "ミオ改") {
		t.Fatalf("作成者による追加席の変更に失敗しました: %d %s", updated.Code, updated.Body.String())
	}
}
