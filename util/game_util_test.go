package util

import (
	"testing"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
)

func TestApplyUniqueBotNamesAvoidsProfileAndRentalCollisions(t *testing.T) {
	agents := []*model.Agent{
		{Idx: 1, GameName: "あいうえおか"},
		{Idx: 2, GameName: "Agent[02]", BotName: "あいうえおか"},
		{Idx: 3, GameName: "Agent[03]", BotName: "同名"},
		{Idx: 4, GameName: "Agent[04]", BotName: "同名"},
	}

	applyUniqueBotNames(agents)

	if agents[1].GameName != "あいうえ-2" {
		t.Fatalf("collision with profile: got %q", agents[1].GameName)
	}
	if agents[2].GameName != "同名" || agents[3].GameName != "同名-4" {
		t.Fatalf("duplicate rental names: got %q and %q", agents[2].GameName, agents[3].GameName)
	}
}
