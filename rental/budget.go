package rental

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// gpt-5.6-luna の単価（USD/1M tokens）。料金が変わったらここを更新する。
const (
	costPerInputToken  = 0.20 / 1_000_000
	costPerOutputToken = 1.20 / 1_000_000
	// 1回の呼び出しで想定する最大コスト。入力8000・出力1024に余裕を持たせる。
	reservePerCall = 0.004
)

// Budget は日次・試合ごとの利用量を管理し、JSONファイルへ永続化する。
type Budget struct {
	mu       sync.Mutex
	path     string // "" ならメモリのみ
	dailyCap float64
	matchCap float64
	day      string // YYYY-MM-DD（ローカル日付）
	spent    float64
	reserved float64
	matches  map[string]float64
}

type usageFile struct {
	Day      string             `json:"day"`
	SpentUSD float64            `json:"spent_usd"`
	Matches  map[string]float64 `json:"matches,omitempty"`
}

func envUSD(key string) float64 {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// NewBudget はdataDir/rental-usage.jsonから利用量を復元する。
// AIWOLF_RENTAL_DAILY_USD / AIWOLF_RENTAL_MATCH_USD が0・未設定なら受付を止める。
func NewBudget(dataDir string) (*Budget, error) {
	b := &Budget{
		dailyCap: envUSD("AIWOLF_RENTAL_DAILY_USD"),
		matchCap: envUSD("AIWOLF_RENTAL_MATCH_USD"),
		matches:  map[string]float64{},
		day:      today(),
	}
	if b.matchCap <= 0 || b.matchCap > b.dailyCap {
		b.matchCap = b.dailyCap
		if b.matchCap > 1.0 {
			b.matchCap = 1.0
		}
	}
	if dataDir == "" {
		dataDir = filepath.Join("data")
	}
	b.path = filepath.Join(dataDir, "rental-usage.json")
	data, err := os.ReadFile(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return b, err
	}
	var f usageFile
	if err := json.Unmarshal(data, &f); err != nil {
		return b, err
	}
	if f.Day == b.day {
		b.spent = f.SpentUSD
		if f.Matches != nil {
			b.matches = f.Matches
		}
		slog.Info("レンタル利用量を復元しました", "day", f.Day, "spent_usd", f.SpentUSD)
	} else {
		slog.Info("日付が変わったためレンタル利用量をリセットします", "saved", f.Day, "today", b.day)
	}
	return b, nil
}

// NewBudgetInMemory は永続化しないBudgetを返す（読み込み失敗時のフォールバック）。
func NewBudgetInMemory() *Budget {
	b := &Budget{
		dailyCap: envUSD("AIWOLF_RENTAL_DAILY_USD"),
		matches:  map[string]float64{},
		day:      today(),
	}
	b.matchCap = envUSD("AIWOLF_RENTAL_MATCH_USD")
	if b.matchCap <= 0 || b.matchCap > b.dailyCap {
		b.matchCap = b.dailyCap
		if b.matchCap > 1.0 {
			b.matchCap = 1.0
		}
	}
	return b
}

func today() string {
	return time.Now().Format("2006-01-02")
}

func (b *Budget) DailyCap() float64 { return b.dailyCap }
func (b *Budget) MatchCap() float64 { return b.matchCap }

// rollDay は日付が変わっていればリセットする。b.mu 保持中に呼ぶ。
func (b *Budget) rollDay() {
	if t := today(); t != b.day {
		b.day = t
		b.spent = 0
		b.reserved = 0
		b.matches = map[string]float64{}
	}
}

func (b *Budget) DailyExceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollDay()
	return b.dailyCap > 0 && b.spent+b.reserved >= b.dailyCap
}

// MatchSpend は指定ルームの累計費用を返す。
func (b *Budget) MatchSpend(roomID string) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.matches[roomID]
}

// TryReserve は1回分の予算を予約する。予約分を含めて日次上限を超えるなら失敗。
func (b *Budget) TryReserve() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollDay()
	if b.dailyCap <= 0 || b.spent+b.reserved+reservePerCall > b.dailyCap {
		return false
	}
	b.reserved += reservePerCall
	return true
}

// Settle はAPI応答のusageで精算する。usage不明なら予約額をそのまま費用にする。
func (b *Budget) Settle(u usage, roomID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved -= reservePerCall
	if b.reserved < 0 {
		b.reserved = 0
	}
	cost := reservePerCall
	if u.InputTokens > 0 || u.OutputTokens > 0 {
		cost = float64(u.InputTokens)*costPerInputToken + float64(u.OutputTokens)*costPerOutputToken
	}
	b.spent += cost
	b.matches[roomID] += cost
	b.save()
}

// save はJSONへ原子的に書き出す。b.mu 保持中に呼ぶ。
func (b *Budget) save() {
	if b.path == "" {
		return
	}
	f := usageFile{Day: b.day, SpentUSD: b.spent, Matches: b.matches}
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.path), ".rental-usage-*")
	if err != nil {
		slog.Warn("レンタル利用量の一時ファイル作成に失敗しました", "error", err)
		return
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), b.path); err != nil {
		slog.Warn("レンタル利用量の保存に失敗しました", "error", err)
	}
}
