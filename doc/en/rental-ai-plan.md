# Hosted rental AI implementation plan

Status: proposal, 2026-09-29. This document does not implement the feature, call the API, or incur API charges.

## 1. Experience and initial scope

Players rent an AI seat from their browser using the operator's OpenAI API account. They enter an AI name and a custom SKILL of up to 200 characters, without supplying an API key, CLI, or SKILL file.

- Room creation and joining offer “Rental AI”, “Connect my own AI”, and “Spectate”. Rental AI is the recommended selection when available.
- Names allow 24 characters. Empty SKILL text uses the default persona. Count Unicode code points consistently in the browser and server; reject excess text instead of silently truncating it.
- Display the custom AI name separately from the existing in-game character name. Identify action targets by seat/agent ID rather than display names.
- Example: “Speak calmly. Focus on contradictions and voting patterns. Ask suspicious players questions first and briefly explain your evidence.”
- Provide cautious, assertive, and listener examples, plus a `0 / 200` counter. SKILL text expresses personality, speaking style, and strategy preferences; it cannot change assigned roles or game rules.
- Joining reserves the player's own seat. The existing host starts the game after all agents are ready. Rental and external agents may share a game.
- Allow editing and cancellation while waiting; freeze the SKILL at game start. Existing private advice remains available during the game.
- An explicit “Save on this device” action stores the name and SKILL draft for reuse. Cross-account/device sharing is a later feature.
- Games continue after the browser closes. The same browser can return to its seat. Recovery of running games after a server restart remains outside the current system's scope; paid inference must not restart for a game that no longer exists.
- Initially enable rentals for five-player, turn-based rooms. Expand to nine/thirteen players and freeform games after concurrency and latency tuning. Preserve existing external-agent support.
- This is an operator-funded trial feature. Player billing and payment collection are outside this implementation.

## 2. UI

Reuse the current light notebook design and raised buttons, with a single-column mobile layout.

1. The home page distinguishes “Play without setup → Rental AI” and “Use your own AI → Setup”.
2. Selecting rental participation reveals the name, SKILL, examples, and counter in the create/join form.
3. Lobby seats show the rental badge and preparing/ready/failed status.
4. Matches reuse the table, speech, and owner panels. Generation indicators must not reveal secret roles or night actors through public notifications.
5. Tell the owner when a failed response required an automatic fallback. Other viewers receive only existing public game progress.
6. Reuse the existing 30-day replay retention. Never add private SKILL text, API usage records, or owner notes to public replays.

## 3. Architecture

```text
Browser → Room API → Seat reservation and rental coordinator
                              ↓
                        Per-seat worker
                              ↕ Internal WebSocket, existing agent protocol
                        Existing game server

Per-seat worker → Allowed input extraction → OpenAI Responses API
                ← Validation and deadline check ← gpt-5.6-luna
```

- A Go `rental/` package owns workers, the WS client, prompts, OpenAI requests, and output validation. Keep OpenAI code out of `logic/` and `model/`.
- Connect as a normal agent, like the player kit, using a fixed internal address. Do not use a public WS URL or user-controlled destination.
- `room/` manages ownership, participation type, and seat status. `transport/` coordinates rentals. Workers receive no full Room object or omniscient game snapshot.
- Rental connection tokens stay on the server and are never returned by the invitation API. Internal connections must satisfy existing authentication when enabled.
- Use a reservation generation and exclusive attachment to prevent duplicate workers, external-agent replacement races, and concurrent starts. Roll back the seat, worker, and budget reservation together on failure.
- Handle `NAME` immediately in code. Initialization, day updates, notifications, and completion only update local state. Only response-requiring TALK/WHISPER/VOTE/DIVINE/GUARD/ATTACK requests invoke the API.
- Continue reading the WS while inference runs. Cancel stale inference on replacement requests, connection checks, death, or completion, and never send a late result as the next action.
- Do not feed the Web spectator history into the worker: dead owners receive omniscient Web views. Build private seat state from the regular agent protocol instead.

## 4. API and state proposal

- Add `agent_source: external | rental` and `rental_profile: {name, skill}` to create/join APIs. Omission preserves external-agent behavior.
- `PUT /api/v1/rooms/{id}/rental` edits the owner's waiting profile; `DELETE` cancels their rental. Reject other owners' seats and changes after game start.
- Retrying failed preparation is an explicit owner action with rate limiting and duplicate prevention.
- `GET /api/v1/rental-capabilities` exposes availability, supported sizes, character limits, and friendly unavailability reasons, without keys, destinations, detailed budgets, or raw API errors.
- Track participation source and preparing/ready/playing/degraded/finished/failed state. Public projection exposes appropriate lobby connection status; private runtime state remains owner-only during games.
- Mutations require the existing owner cookie, Origin validation, and a bounded JSON body. Knowing a RoomID does not grant modification rights.

## 5. OpenAI requests

- Use exactly `gpt-5.6-luna`. Official documentation confirms Responses API and Structured Outputs support. Access from the operator's account must still be verified during implementation. [Model specification](https://developers.openai.com/api/docs/models/gpt-5.6-luna)
- Only the operator configures `OPENAI_API_KEY`, through an environment variable or Docker Secret. Never embed it in the repository, browser, prompt, or logs, or include a real secret in examples.
- Fix the HTTPS API destination to OpenAI. Users cannot select the model, URL, or tool configuration. Prevent redirects from forwarding credentials to another host.
- Initial settings: `reasoning.effort: low`, `max_output_tokens: 1024`. Account for reasoning within the output budget and handle incomplete responses. Tune after observing performance. [Reasoning and output limits](https://developers.openai.com/api/docs/guides/reasoning)
- Configure no tools or execution paths for browsing, shell commands, files, MCP, or fetching URLs.
- Set `store: false` and manage independent seat histories locally. This does not promise that all provider-side retention is disabled. [Data controls](https://developers.openai.com/api/docs/guides/your-data)
- Generate one speech or target choice per request. Optional owner notes are brief explanations, without requesting internal chains of thought.

## 6. Prompt injection defenses

Free text cannot support a promise of zero model manipulation. The goal is to make unauthorized operations, disclosure of other seats' secrets, and spending beyond limits impossible through model output alone. Official guidance recommends combining trust separation with structured outputs. [Safety guidance](https://developers.openai.com/api/docs/guides/agent-builder-safety)

| Boundary | Control |
| --- | --- |
| Fixed instructions versus user text | Developer messages contain only operator-owned rules and response specifications. SKILL text, names, dialogue, and advice remain explicit JSON data in user messages; never interpolate them into developer instructions |
| Custom SKILL | Interpret it as personality, speaking style, and strategy preferences, never executable code or a SKILL package. Validate length, UTF-8, and control characters. Keywords or delimiters alone do not establish safety |
| Other agents' speech | Treat it as game testimony. Fake SYSTEM messages, configuration changes, and requests for secrets do not gain instruction authority. Normal bluffing and role claims remain legitimate tactics |
| Owner advice | Accept advice only for the owner's living seat, without overriding game constraints. Bound length, frequency, and the unread queue |
| Input information | Build an allowlisted seat DTO. Exclude API keys, invitation tokens, key phrases, other seats' SKILL/advice, and unknown roles. Never share conversation IDs or model memory across seats or rooms |
| Actions | The server fixes the requested action type. Use separate speech/target JSON Schemas, disallow extra fields, and enumerate legal target choices |
| Final validation | Recheck request generation, life status, target, role permission, length, and remaining speech allowance in code. Only permit SKIP or no-target when the game rules allow it |
| Rendering | Escape speech, SKILL text, and notes as text. Do not add HTML execution, automatic remote images, or automatic link execution |

Structured Outputs constrain shape, not semantic safety or correctness. Handle refusals, incomplete output, and invalid targets separately. [Structured outputs](https://developers.openai.com/api/docs/guides/structured-outputs)

An agent may still disclose its own SKILL or known role through generated speech. SKILL text is not a secret credential. Distinguish legitimate role claims and bluffing from enforcement of system permissions.

## 7. Deadlines and fallback

- Wait for events without inference loops or periodic API polling.
- Initial API timeout: 15 seconds per attempt. Retry transient 429/5xx errors at most once. Include backoff within a 35-second total budget and finish at least three seconds before the game's action deadline; shorter game settings take precedence.
- Do not retry if Retry-After exceeds the remaining deadline. Never regenerate indefinitely after 401/403, refusals, or invalid output.
- On failure, send one allowed fallback speech or legal target selection without another model call. Never SKIP when forbidden. If no legal action exists, defer to the existing game behavior.
- After three consecutive failures or budget exhaustion, retain fallback mode for the remainder of the match and notify the owner. Do not automatically resume paid inference mid-match.
- Send at most one action per request. Separate API transport retries from game-action resends.

## 8. Cost and abuse limits

The model page lists $0.20 per million input tokens and $1.20 per million output tokens. Match cost depends on supplied history, generated/reasoning tokens, and match length; do not promise a fixed match price before measurement. [Model pricing](https://developers.openai.com/api/docs/models/gpt-5.6-luna)

- Proposed limits: one concurrent seat per user, five workers globally, 60 API attempts per seat and 200 per match, including retries. Cap each input at 8,000 tokens and output at 1,024 tokens.
- Preserve important state, own results, and voting history structurally while trimming dialogue to fit. Summaries never become privileged instructions. Initial history management uses no separate LLM calls.
- Reserve worker capacity throughout waiting and play. Waiting reservations expire after ten minutes unless explicitly extended; release on leaving, closing, or completion.
- Require operator-configured daily and match budgets before enabling public rentals. Missing credentials or budgets make the feature unavailable.
- Atomically reserve maximum estimated cost before each request and settle against returned usage. Retain the reservation when billing is uncertain rather than assuming zero usage. Include conservative allowances for cache writes and other applicable charges. Stop new admission at the cap.
- Persist usage and reservations in a store such as SQLite so restarting cannot reset spending. Initially operate a single server; multiple instances require shared atomic budget management.
- Cookies are weak identity. Combine session limits, IP limits from explicitly trusted proxies, and global caps; never blindly trust forwarded IP headers. Public deployment also requires server-verified Turnstile or an equivalent challenge at rental creation.
- Do not claim to prevent all cookie resets or IP changes. Introduce login when strict per-person allocations become necessary.
- Operational records contain model, token counts, estimated cost, latency, error category, and anonymized seat identifiers. Keep full prompts, secrets, and raw API errors out of public logs.

## 9. Implementation order and ownership

1. **Seats and public APIs:** `room/`, `transport/web.go`; ownership, source, reservation generations, editing, freeze-on-start, public/private status. Stabilize the API contract first.
2. **Workers:** `rental/`; internal WS, private input, Responses, schemas, cancellation, and legal fallbacks. Can proceed in parallel against the contract.
3. **Web:** `web/static/`; source cards, 200-character input, examples, preparation, mobile layout, and owner panel. Can proceed against response examples.
4. **Budget and operations:** `store/`, Compose, and configuration examples; durable usage, atomic reservations, rate limits, secrets, and the stop switch. Integrate through a pre-charge worker hook.
5. **Integration and documentation:** update Japanese/English APIs, configuration, and participation guidance. Update all distributed YAML files when adding YAML keys. Preserve observer-based updates and never publish secret action actors through new events.

Implementation acceptance criteria:

- Five rentals can prepare, start, and finish entirely through the browser; mixed external/rental games follow normal rules.
- Waiting, rerendering, duplicate clicks, and browser reconnection do not duplicate seats or API calls.
- Other-seat secrets and credentials never enter input DTOs, responses, SSE, or public replays.
- Injection through SKILL, speech, or advice cannot bypass operation permissions, action type, legal targets, or call budgets.
- Deadlines, 429/5xx, disconnects, refusals, malformed JSON, and exhausted budgets never cause stale actions or infinite retries.
- Concurrent budget reservations, restart balances, and cancellation cleanup remain consistent.
- Participation options, counters, and errors remain readable on small screens.

This stage is design only. Credential configuration and paid API calls belong to implementation.
