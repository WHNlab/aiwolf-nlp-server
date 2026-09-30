package rental

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// APIError は秘密を含み得る生のAPI本文を保存せず、公開可能な分類だけを保持する。
type APIError struct {
	Status int
	Kind   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("OpenAI APIエラー (status=%d, kind=%s)", e.Status, e.Kind)
}

func classifyAPIError(status int, data []byte) *APIError {
	var body struct {
		Error struct {
			Code string
			Type string
		}
	}
	_ = json.Unmarshal(data, &body)
	kind := "request"
	switch body.Error.Code {
	case "insufficient_quota", "credit_balance_exhausted", "organization_spend_limit_exceeded",
		"project_spend_limit_exceeded", "organization_usage_limit_exceeded", "billing_hard_limit_reached", "billing_not_active":
		kind = "quota"
	case "model_not_found":
		kind = "model"
	}
	switch {
	case body.Error.Type == "insufficient_quota":
		kind = "quota"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		kind = "auth"
	case kind == "request" && status == http.StatusTooManyRequests:
		kind = "rate_limit"
	case status >= 500:
		kind = "server"
	}
	return &APIError{Status: status, Kind: kind}
}

func (e *APIError) retryable() bool {
	return e.Kind == "rate_limit" || e.Kind == "server"
}

func (e *APIError) permanent() bool {
	return !e.retryable()
}

func rentalErrorMessage(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Kind {
		case "quota":
			return "AI提供元の利用枠が不足しています。管理者がOpenAIの残高・請求設定・利用上限を確認してください。"
		case "auth":
			return "AI提供元の認証に失敗しました。管理者がAPIキーとアクセス権限を確認してください。"
		case "model":
			return "設定されたAIモデルを利用できません。管理者がモデルの利用権限を確認してください。"
		case "rate_limit":
			return "AI提供元が混み合っており、応答できませんでした。少し待ってから再試行してください。"
		case "server":
			return "AI提供元で一時的な障害が発生しています。少し待ってから再試行してください。"
		default:
			return "AIへの要求を受け付けられませんでした。管理者がAPI設定を確認してください。"
		}
	}
	if errors.Is(err, errBudget) {
		return "レンタルAIの利用上限に達しました。"
	}
	return "AIの応答を確認できませんでした。時間をおいて再試行してください。"
}

var errBudget = errors.New("レンタルAIの利用上限に達しました")
