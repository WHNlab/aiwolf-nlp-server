package observer

import (
	"encoding/json"

	"github.com/aiwolfdial/aiwolf-nlp-server/model"
)

// ゲームのセマンティックなイベントを各 sink（ロガー・ブロードキャスタ等）へ通知する。
// CSV書式やパケット組み立てといった整形は logic ではなく各 sink 側で行う。
// modelのみに依存する葉パッケージとし、渡す値はすべて読み取り専用。
type GameObserver interface {
	OnGameStart(id string, agents []model.AgentView, state model.GameState)
	OnGameEnd(id string, winSide model.Team, state model.GameState)

	OnDayStatus(id string, day int, statuses []model.AgentStatus)
	OnResult(id string, day int, villagers int, werewolves int, winSide model.Team)

	OnTalk(id string, day int, request model.Request, talk model.TalkView, voiceID *int, state model.GameState)
	OnFreeformTalk(id string, agent model.AgentView, request model.Request, talk model.TalkView)
	OnPhase(id string, request model.Request)
	OnPublicPhase(id string, phase model.PublicPhase, day int)
	OnPublicTurnStart(id string, turn model.PublicTurnView)
	OnPublicTurnEnd(id string, turnID string)
	OnRequest(id string, agent model.AgentView, request json.RawMessage)
	OnResponse(id string, agent model.AgentView, response string, err error)

	OnVote(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState)
	OnAttackVote(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState)
	OnExecute(id string, day int, executed *model.AgentView, state model.GameState)
	OnDivine(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState)
	OnGuard(id string, day int, agent model.AgentView, target model.AgentView, state model.GameState)
	OnAttack(id string, day int, attacked *model.AgentView, guarded bool, state model.GameState)

	// AIから所有者への個別メッセージ。対応クライアントが {"response","note"} 形式で
	// 応答した場合に note を通知する。ゲーム進行には影響しない。
	OnOwnerMessage(id string, day int, agent model.AgentView, message string)
}
