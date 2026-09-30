package model

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBotNameLength = 6

var ErrInvalidBotName = errors.New("Bot名（チーム名）は制御文字を含まない1〜6文字にしてください。Over・Skip・Noneは使用できません")

// NormalizeBotName はWebの入力とNAME応答で同じ文字数・予約語の制約を適用する。
func NormalizeBotName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > MaxBotNameLength {
		return "", ErrInvalidBotName
	}
	if name == T_OVER || name == T_SKIP || name == "None" {
		return "", ErrInvalidBotName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrInvalidBotName
		}
	}
	return name, nil
}
