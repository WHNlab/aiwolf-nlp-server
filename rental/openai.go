package rental

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	modelName       = "gpt-5.6-luna"
	responsesURL    = "https://api.openai.com/v1/responses"
	maxOutputTokens = 1024
	maxRetries      = 1
)

type openAIClient struct {
	apiKey string
	http   *http.Client
}

func newOpenAIClient(apiKey string) *openAIClient {
	return &openAIClient{
		apiKey: apiKey,
		http: &http.Client{
			// リダイレクトで認証ヘッダを別ホストへ送らない。
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// genOutput はStructured Outputsで検証済みの応答。speechかtargetのどちらかが入る。
type genOutput struct {
	speech string
	target string
	note   string
}

type usage struct {
	Known        bool  `json:"-"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// jsonSchema はResponses APIの構造化出力定義。
type jsonSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

// schemaFor は要求種別に応じたスキーマを返す。
// 発言系は {"speech","note"}、対象系は {"target","note"}（targetは候補enum）。
func schemaFor(request string, candidates []string) jsonSchema {
	noteProp := map[string]any{
		"type":        "string",
		"description": "所有者への短いメモ（任意。空文字可）",
	}
	switch request {
	case "TALK", "WHISPER":
		return jsonSchema{
			Name:   "wolf_speech",
			Strict: true,
			Schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"speech": map[string]any{
						"type":        "string",
						"description": "ゲームに返す発言本文。発言を終えるときは Over、発言を飛ばすときは Skip。",
					},
					"note": noteProp,
				},
				"required": []string{"speech", "note"},
			},
		}
	default:
		enum := append([]string{}, candidates...)
		return jsonSchema{
			Name:   "wolf_target",
			Strict: true,
			Schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"target": map[string]any{
						"type":        "string",
						"description": "選択する対象の名前。候補リストの値だけを使う。",
						"enum":        enum,
					},
					"note": noteProp,
				},
				"required": []string{"target", "note"},
			},
		}
	}
}

type reqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesRequest struct {
	Model           string       `json:"model"`
	Store           bool         `json:"store"`
	MaxOutputTokens int          `json:"max_output_tokens"`
	Reasoning       reasoningReq `json:"reasoning"`
	Input           []reqMessage `json:"input"`
	Text            textFormat   `json:"text"`
}

type reasoningReq struct {
	Effort string `json:"effort"`
}

type textFormat struct {
	Format formatDef `json:"format"`
}

type formatDef struct {
	Type   string         `json:"type"` // "json_schema"
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

// responsesResponse はResponses APIの応答のうち利用する部分。
type responsesResponse struct {
	Status            string `json:"status"`
	OutputText        string `json:"output_text"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage  *usage `json:"usage"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// extractText はoutput_textまたはoutput配列からテキストを取り出す。
// refusalのみの出力はエラー扱いする。
func (r *responsesResponse) extractText() (string, error) {
	if r.Error != nil || (r.Status != "" && r.Status != "completed") {
		return "", errors.New("AIの生成が完了しませんでした")
	}
	if r.OutputText != "" {
		return r.OutputText, nil
	}
	for _, item := range r.Output {
		for _, c := range item.Content {
			if c.Type == "output_text" && c.Text != "" {
				return c.Text, nil
			}
			if c.Type == "refusal" {
				return "", errors.New("モデルが応答を拒否しました")
			}
		}
	}
	return "", errors.New("応答テキストが空です")
}

// reserveCall は送信ごとに予算を確保し、応答後の精算関数を返す。
type reserveCall func() (func(usage), error)

// generate はゲームの期限と35秒の上限内で生成する。一時的な429/5xxだけ1回再試行する。
func (c *openAIClient) generate(ctx context.Context, input []reqMessage, schema jsonSchema, deadline time.Time, reserve reserveCall) (*genOutput, error) {
	if c == nil {
		return nil, errors.New("レンタルAIが無効です")
	}
	apiDeadline := deadline.Add(-actionSafetyMargin)
	if cap := time.Now().Add(35 * time.Second); cap.Before(apiDeadline) {
		apiDeadline = cap
	}
	ctx, cancel := context.WithDeadline(ctx, apiDeadline)
	defer cancel()
	body := responsesRequest{
		Model: modelName, Store: false, MaxOutputTokens: maxOutputTokens,
		Reasoning: reasoningReq{Effort: "low"}, Input: input,
		Text: textFormat{Format: formatDef{Type: "json_schema", Name: schema.Name, Strict: schema.Strict, Schema: schema.Schema}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, errors.New("AIへの要求を作成できませんでした")
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		settle, err := reserve()
		if err != nil {
			return nil, err
		}
		out, u, retryAfter, err := c.request(ctx, raw)
		settle(u)
		if err == nil {
			text, err := out.extractText()
			if err != nil {
				return nil, err
			}
			parsed := parseOutput(text, schema)
			if parsed == nil {
				return nil, errors.New("AIの応答形式が不正です")
			}
			return parsed, nil
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || !apiErr.retryable() || attempt >= maxRetries {
			return nil, err
		}
		wait := parseRetryAfter(retryAfter)
		if wait <= 0 {
			wait = time.Second + time.Duration(rand.IntN(250))*time.Millisecond
		}
		// 待機後に送信する余裕がなければ、追加の課金を始めない。
		if wait+time.Second >= time.Until(apiDeadline) {
			return nil, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *openAIClient) request(ctx context.Context, raw []byte) (*responsesResponse, usage, string, error) {
	ctx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responsesURL, bytes.NewReader(raw))
	if err != nil {
		return nil, usage{}, "", errors.New("AIへの要求を作成できませんでした")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, usage{}, "", ctx.Err()
		}
		return nil, usage{}, "", errors.New("AI提供元へ接続できませんでした")
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// 明示的に拒否された4xxは生成費用に数えない。通信断などの不明分は予約額を維持する。
		u := usage{Known: resp.StatusCode >= 400 && resp.StatusCode < 500}
		return nil, u, resp.Header.Get("Retry-After"), classifyAPIError(resp.StatusCode, data)
	}
	if readErr != nil {
		return nil, usage{}, "", errors.New("AIの応答を読み取れませんでした")
	}
	var out responsesResponse
	if json.Unmarshal(data, &out) != nil {
		return nil, usage{}, "", errors.New("AIの応答を解析できませんでした")
	}
	u := usage{}
	if out.Usage != nil {
		u = *out.Usage
		u.Known = true
	}
	return &out, u, "", nil
}

func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs >= 0 && !math.IsNaN(secs) && !math.IsInf(secs, 0) {
		return time.Duration(math.Min(secs, 86400) * float64(time.Second))
	}
	if until, err := http.ParseTime(v); err == nil {
		return time.Until(until)
	}
	return 0
}

// parseOutput は必須キー・型・対象候補を検証し、不正な出力を成功として扱わない。
func parseOutput(text string, schema jsonSchema) *genOutput {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil || len(m) != 2 {
		return nil
	}
	note, ok := m["note"].(string)
	if !ok {
		return nil
	}
	out := &genOutput{note: note}
	if schema.Name == "wolf_speech" {
		v, ok := m["speech"].(string)
		if !ok || strings.TrimSpace(v) == "" {
			return nil
		}
		out.speech = strings.TrimSpace(v)
		return out
	}
	target, ok := m["target"].(string)
	if !ok {
		return nil
	}
	props := schema.Schema["properties"].(map[string]any)
	candidates := props["target"].(map[string]any)["enum"].([]string)
	for _, candidate := range candidates {
		if target == candidate {
			out.target = target
			return out
		}
	}
	return nil
}
