package rental

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		if request == "ATTACK" {
			enum = append(enum, "None")
		}
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
	Usage  usage `json:"usage"`
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
	if r.Error != nil && r.Error.Message != "" {
		return "", fmt.Errorf("APIエラー: %s", r.Error.Code)
	}
	if r.OutputText != "" {
		return r.OutputText, nil
	}
	for _, item := range r.Output {
		for _, c := range item.Content {
			if c.Type == "output_text" && c.Text != "" {
				return c.Text, nil
			}
			if c.Type == "refusal" && c.Text != "" {
				return "", errors.New("モデルが応答を拒否しました")
			}
		}
	}
	if r.Status == "incomplete" {
		reason := "unknown"
		if r.IncompleteDetails != nil {
			reason = r.IncompleteDetails.Reason
		}
		return "", fmt.Errorf("出力が途中で打ち切られました: %s", reason)
	}
	return "", errors.New("応答テキストが空です")
}

// generate は1回の行動生成を行う。deadlineはゲームのaction期限で、
// その3秒前までに応答が返らなければ諦める。429/5xxのみ1回だけ再試行する。
func (c *openAIClient) generate(ctx context.Context, input []reqMessage, schema jsonSchema, deadline time.Time) (*genOutput, usage, error) {
	var u usage
	if c == nil {
		return nil, u, errors.New("レンタルAIが無効です")
	}
	// ゲームの期限に触れないよう安全側の締切を設ける。
	apiDeadline := deadline.Add(-actionSafetyMargin)
	if remain := time.Until(apiDeadline); remain <= 0 {
		return nil, u, errors.New("応答期限が残っていません")
	} else if remain < apiCallTimeout {
		// 残りが短いときはゲームの期限に合わせる。
		apiDeadline = time.Now().Add(remain)
	} else {
		apiDeadline = time.Now().Add(apiCallTimeout)
	}

	body := responsesRequest{
		Model:           modelName,
		Store:           false,
		MaxOutputTokens: maxOutputTokens,
		Reasoning:       reasoningReq{Effort: "low"},
		Input:           input,
		Text: textFormat{Format: formatDef{
			Type:   "json_schema",
			Name:   schema.Name,
			Strict: schema.Strict,
			Schema: schema.Schema,
		}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, u, err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if time.Until(apiDeadline) <= 0 {
			return nil, u, errors.New("応答期限に間に合いませんでした")
		}
		reqCtx, cancel := context.WithDeadline(ctx, apiDeadline)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, responsesURL, bytes.NewReader(raw))
		if err != nil {
			cancel()
			return nil, u, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.http.Do(req)
		if err != nil {
			cancel()
			if reqCtx.Err() != nil || ctx.Err() != nil {
				return nil, u, errors.New("応答期限に間に合いませんでした")
			}
			lastErr = err
			break
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		retryAfter := resp.Header.Get("Retry-After")
		status := resp.StatusCode
		resp.Body.Close()
		cancel()
		if readErr != nil {
			lastErr = readErr
			break
		}
		if status == http.StatusOK {
			var out responsesResponse
			if err := json.Unmarshal(data, &out); err != nil {
				return nil, u, fmt.Errorf("応答の解析に失敗しました: %w", err)
			}
			u = out.Usage
			text, err := out.extractText()
			if err != nil {
				return nil, u, err
			}
			return parseOutput(text, schema), u, nil
		}
		// 認証系・4xxは再試行しない。429/5xxだけ1回まで。
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, u, fmt.Errorf("APIキーが無効です(%d)", status)
		}
		retryable := status == http.StatusTooManyRequests || status >= 500
		lastErr = fmt.Errorf("OpenAI APIが%dを返しました", status)
		if !retryable || attempt >= maxRetries {
			break
		}
		if wait := parseRetryAfter(retryAfter); wait > 0 {
			if wait > time.Until(apiDeadline) {
				break
			}
			select {
			case <-ctx.Done():
				return nil, u, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return nil, u, lastErr
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// parseOutput は構造化出力のJSONを genOutput へ写す。
func parseOutput(text string, schema jsonSchema) *genOutput {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return nil
	}
	out := &genOutput{}
	if v, ok := m["note"].(string); ok {
		out.note = v
	}
	if v, ok := m["speech"].(string); ok {
		out.speech = strings.TrimSpace(v)
	}
	if v, ok := m["target"].(string); ok {
		out.target = strings.TrimSpace(v)
	}
	if out.speech == "" && out.target == "" {
		return nil
	}
	return out
}
