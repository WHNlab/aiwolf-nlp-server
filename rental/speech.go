package rental

import (
	"strings"
	"unicode/utf8"

	"github.com/aiwolfdial/aiwolf-nlp-server/util"
)

type speechRules struct {
	MaxLength    *int `json:"max_length,omitempty"`
	CountInWords bool `json:"count_in_words"`
	CountSpaces  bool `json:"count_spaces"`
}

// speechRulesFor は実際の発言設定と残り文字数から、メンションの加算に頼らず書ける上限を求める。
func speechRulesFor(setting *settingIn, request string, remain *int) speechRules {
	ts := setting.Talk
	if request == "WHISPER" {
		ts = setting.Whisper
	}
	ml := ts.MaxLength
	rules := speechRules{}
	if ml.CountInWord != nil {
		rules.CountInWords = *ml.CountInWord
	}
	if ml.CountSpaces != nil {
		rules.CountSpaces = *ml.CountSpaces
	}
	apply := func(limit int) {
		limit = max(0, limit)
		if rules.MaxLength == nil || limit < *rules.MaxLength {
			rules.MaxLength = &limit
		}
	}
	if ml.PerTalk != nil && *ml.PerTalk >= 0 {
		apply(*ml.PerTalk)
	}
	if ml.BaseLength != nil || ml.PerAgent != nil {
		limit := 0
		if ml.BaseLength != nil {
			limit += max(0, *ml.BaseLength)
		}
		if ml.PerAgent != nil {
			remaining := *ml.PerAgent
			if remain != nil {
				remaining = *remain
			}
			limit += max(0, remaining)
		}
		apply(limit)
	}
	return rules
}

// validSpeech は文として完結した部分を残す。制約内の本文には独自の200文字制限を掛けない。
func validSpeech(s string, rules speechRules, remainSkip *int) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "Skip" && remainSkip != nil && *remainSkip <= 0 {
		return "Over"
	}
	if s == "Over" || s == "Skip" || rules.MaxLength == nil {
		return s
	}
	limit := *rules.MaxLength
	if limit <= 0 {
		return "Over"
	}
	if util.CountLength(s, rules.CountInWords, rules.CountSpaces) <= limit {
		return s
	}
	prefix := util.TrimLength(s, limit, rules.CountInWords, rules.CountSpaces)
	end := 0
	for i, r := range prefix {
		if strings.ContainsRune("。！？!?", r) || r == '.' && (i+1 == len(prefix) || prefix[i+1] == ' ') {
			end = i + utf8.RuneLen(r)
		}
	}
	if end > 0 {
		return strings.TrimSpace(prefix[:end])
	}
	// 一文も収まらない場合は省略を明示し、末尾が欠けた文をそのまま表示しない。
	return strings.TrimSpace(util.TrimLength(s, limit-1, rules.CountInWords, rules.CountSpaces)) + "…"
}
