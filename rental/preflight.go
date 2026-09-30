package rental

import (
	"context"
	"errors"
	"time"
)

const readinessCacheTTL = time.Minute

// preflight は実際のモデル・出力形式で確認してから接続する。同時入室の確認は1回にまとめる。
func (m *Manager) preflight(ctx context.Context, roomID string) error {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.checkMu.Lock()
		if time.Now().Before(m.checkUntil) {
			err := m.checkErr
			m.checkMu.Unlock()
			return err
		}
		if pending := m.checking; pending != nil {
			m.checkMu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		m.checking = pending
		m.checkMu.Unlock()
		_, err := m.client.generate(ctx, []reqMessage{{Role: "developer", Content: "接続確認です。speechに「準備できました」、noteに空文字を入れて返してください。"}}, schemaFor("TALK", nil), time.Now().Add(35*time.Second+actionSafetyMargin), func() (func(usage), error) {
			return m.budget.Reserve(roomID)
		})
		m.checkMu.Lock()
		// 退室による中止・部屋ごとの予算不足は他の部屋へ引き継がない。
		if ctx.Err() == nil && !errors.Is(err, errBudget) {
			m.checkErr = err
			m.checkUntil = time.Now().Add(readinessCacheTTL)
		}
		m.checking = nil
		close(pending)
		m.checkMu.Unlock()
		return err
	}
}

// providerError は課金・認証など全席に共通する障害だけを短時間共有する。
func (m *Manager) providerError() error {
	m.checkMu.Lock()
	defer m.checkMu.Unlock()
	var apiErr *APIError
	if time.Now().Before(m.checkUntil) && errors.As(m.checkErr, &apiErr) && sharedProviderError(apiErr) {
		return apiErr
	}
	return nil
}

func (m *Manager) recordProviderError(err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !sharedProviderError(apiErr) {
		return
	}
	m.checkMu.Lock()
	m.checkErr = err
	m.checkUntil = time.Now().Add(readinessCacheTTL)
	m.checkMu.Unlock()
}

func sharedProviderError(err *APIError) bool {
	return err.Kind == "quota" || err.Kind == "auth" || err.Kind == "model"
}
