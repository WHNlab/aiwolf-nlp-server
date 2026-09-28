# Web UI design and SubAgent implementation brief

[日本語](../ja/web-ui-design.md)

Status: proposed specification, not yet implemented. 2026-09-28. Distinguish existing functionality from proposed contracts. This document is shared by UI and backend implementers; working mocks do not establish that the server feature exists.

## 1. Purpose and decisions

People create rooms on the Web, invite their own and their friends' AI agents, read the game from their own AI's perspective, and optionally consult that AI privately. When their AI dies, they receive an omniscient view. Conversation remains available after the game.

- Web, HTTP API, and SSE: port 8080. AI gameplay WebSocket: port 8081.
- Light only. Keep backgrounds, controls, and dialogs light even under an OS dark preference. No theme switch. Night phases do not darken the entire interface.
- Interpret “Note-like” through the reference site's paper notebook, sticky notes, and tactile raised buttons.
- People enter a display name. Keep display name, internal UserID, seat ID, AgentID, RoomID, and GameID distinct.
- Initially support 5, 9, and 13 AI players using matching presets. Offer only combinations the server actually supports.
- The host starts a ready room. Living owners see only their AI's knowledge; deceased owners receive an omniscient view. Hosting does not grant access to secrets.
- People advise their AI; the AI submits game speech, votes, and abilities under the existing rules.
- Initially one game per room. “Play again” creates a new room with fresh seats, invitations, and phrases.

## 2. Reference and adaptations

Reference: [BioQuiz](https://bioquiz.yokohide0317.workers.dev/). Its public HTML and linked [notebook CSS](https://bioquiz.yokohide0317.workers.dev/_astro/notebook.BPoVH1l-.css) were inspected. The later notebook stylesheet overrides `/css/style.css`; do not base the design on the earlier stylesheet alone. The hashed CSS URL may change.

Observed properties include cream backgrounds, graph paper, dark outlines, pastel accents, layered paper edges, and hard 4–5px button shadows that compress on press. Colors, dimensions, and layouts below are project-specific proposals, not a complete reproduction. Browser screenshot capture was unavailable; the reference was inspected through public HTML/CSS rather than visually verified screenshots.

Design concept: “a notebook for watching AI werewolf games.” Provisional Japanese product name: 「人狼ノート」, with “AIWOLF” as secondary text. Make the name configurable in one place.

| Element | Direction |
| --- | --- |
| Page background | Warm cream with a very subtle 26px grid |
| Main panels | Near-white paper, dark outline, a restrained layered bottom edge |
| Headings | Bold Japanese sans-serif; readable regular sans-serif body |
| Actions | Mint raised primary buttons; smaller paper edges on white secondary buttons |
| State | Color plus icon plus text; sticky notes only for meaningful labels |
| Conversation | Plain backgrounds behind text; left-aligned transcript supporting long Japanese text |
| Decoration | Grid around panels; binder holes and red margins only on covers or major notebook frames |

Concentrate character in paper treatment and tactile actions. Do not put heavy outlines and shadows around every utterance. Noninteractive cards must not lift on hover. Never encode hidden roles in colors, images, sorting, or CSS classes.

## 3. Design tokens

Define these in one shared `tokens.css` equivalent. Avoid adding slightly different colors per page.

```css
:root {
  color-scheme: only light;
  --canvas: #f5f0e4;
  --paper: #fffdf6;
  --paper-inset: #f0ebdd;
  --white: #ffffff;
  --ink: #2c3340;
  --ink-muted: #626b77;
  --edge: #333a46;
  --rule: #ddd8ca;
  --grid: rgb(98 126 168 / 10%);
  --paper-edge: #d8d1bf;
  --mint: #9be0be;
  --mint-edge: #559874;
  --mint-ink: #204c37;
  --yellow: #ffdfa0;
  --blue: #d5eafb;
  --purple: #e6dcfa;
  --pink: #f8dce0;
  --danger-ink: #9d3434;
  --success-ink: #286144;
  --focus: #235d96;
  --radius-sm: 8px;
  --radius-control: 12px;
  --radius-panel: 18px;
  --shadow-paper: 0 4px 0 var(--paper-edge);
  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-5: 24px;
  --space-6: 32px;
  --space-7: 48px;
}
```

- Body: system-ui, Hiragino Sans, Noto Sans JP, sans-serif. 16px, line-height 1.8, weight 400–500.
- Headings: the same stack, weight 700–800. Do not require external fonts. If adding a font such as Zen Kaku Gothic New later, verify its license before distribution.
- H1: desktop 32px, mobile 26px. H2: 22/20px. H3: 18px. Metadata: 13–14px with 1.5 line-height.
- Use monospace only for short codes such as RoomID, not body text or role names.
- Aim for roughly 28–38 full-width characters per conversation line; cap desktop text width at 680px.
- Outer borders 2px, inner separators 1px. Selections need checks or underlines in addition to background color.
- Icons: 18–20px outline SVG, approximately 1.8px stroke, `currentColor`. Do not rely exclusively on emoji for meaning.
- Role colors appear only where role access is granted. Undisclosed participants use the same neutral 「非公開」 (“private”) label.

## 4. Raised button specification

Standard height 48px; main start/create actions 52px. Even visually small copy controls need at least a 44px touch area. Horizontal padding 18–24px, radius 12px, border 2px. Normally one primary action per region.

| State | Primary button appearance and behavior |
| --- | --- |
| Rest | Mint face, dark text, hard 5px bottom shadow |
| Hover | Lift 1px, shadow 6px; hover-capable devices only |
| Active | Move down 4px, shadow 1px, without changing document layout |
| Focus-visible | Separate blue 3px outline, 3px offset |
| Disabled | Inset paper fill, readable muted text, 1px shadow; explain why nearby |
| Loading | Keep width stable, use copy such as 「作成中…」, prevent duplicate submission |

```css
.button {
  --button-face: var(--white);
  --button-depth: var(--paper-edge);
  --button-ink: var(--ink);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 48px;
  padding: 10px 20px;
  border: 2px solid var(--edge);
  border-radius: var(--radius-control);
  color: var(--button-ink);
  background: var(--button-face);
  box-shadow: 0 5px 0 var(--button-depth);
  font: inherit;
  font-weight: 700;
  line-height: 1.4;
  cursor: pointer;
  touch-action: manipulation;
  transition: transform 80ms ease, box-shadow 80ms ease;
}
.button--primary {
  --button-face: var(--mint);
  --button-depth: var(--mint-edge);
  --button-ink: var(--mint-ink);
}
@media (hover: hover) {
  .button:hover:not(:disabled):not([aria-disabled="true"]) {
    transform: translateY(-1px);
    box-shadow: 0 6px 0 var(--button-depth);
  }
}
.button:active:not(:disabled):not([aria-disabled="true"]) {
  transform: translateY(4px);
  box-shadow: 0 1px 0 var(--button-depth);
}
.button:focus-visible {
  outline: 3px solid var(--focus);
  outline-offset: 3px;
}
.button:disabled, .button[aria-disabled="true"] {
  background: var(--paper-inset);
  color: var(--ink-muted);
  box-shadow: 0 1px 0 var(--paper-edge);
  cursor: not-allowed;
}
@media (prefers-reduced-motion: reduce) {
  .button { transition: none; }
  .button:hover, .button:active { transform: none !important; }
}
```

Secondary buttons are white. Destructive actions use pale pink with dark red text. Put “Close room” inside a menu. Use links for navigation and buttons for actions. `aria-disabled` does not itself prevent activation; handlers must also reject activation. CSS and SVG suffice for initial decorations; image generation is unnecessary.

## 5. Screens, URLs, and layouts

| URL | Content |
| --- | --- |
| `/` | Create, enter RoomID, rooms joined on this device |
| `/rooms/new` | Room creation form |
| `/rooms/:roomId` | Name entry before joining; then lobby → game → results |

Keep the room URL unchanged across states. Go must serve deep links and reloads correctly. Distinguish nonexistent rooms, closed rooms, and aborted games.

### Desktop: 1200px and above

Maximum width 1440px, horizontal gutters 32px, header 64px. Game columns: 220px / minmax(0, 1fr) / 320px, gaps 24px; preserve at least 480px for the center. The lobby uses a main region plus a 320px invitation/start column. Forms have a 640px maximum width.

```text
┌ Werewolf Notebook   Friday's five-player room [Live]       Yuki ▾ ┐
│ Day 2 / Discussion    Alive 4 / 5       ● Connected      [Share] │
├──────────────┬─────────────────────────┬────────────────────────┤
│ Participants │ Conversation record     │ Your AI                │
│              │ [Public] [Events]       │ Haru / Your AI         │
│ Haru  Yours  │                         │ Role: Seer             │
│ Mio   Alive  │ ── Day 2 / Morning ──   │ [Private information]  │
│ Ren   Alive  │                         │                        │
│ Sora  Dead   │ Haru 09:42              │ Divination             │
│ Nagi  Alive  │ About yesterday's vote… │ Mio → Human            │
│              │                         │                        │
│ [Rules]      │ Mio 09:43               │ Consult your AI        │
│              │ Here's what I think…    │ …                      │
│              │                         │ [Write advice…       ] │
│              │       [3 new messages ↓]│ [Send to AI]           │
└──────────────┴─────────────────────────┴────────────────────────┘
```

The role and divination result are examples for the authorized owner's view. Neither may be transmitted or rendered in a public view. Times in the diagram are real-world times, distinct from the game day. Product copy is Japanese; English here explains the layout.

### Tablet: 768–1199px

Conversation plus a 280px right column, with 16px gaps. Move participants to a panel opened by a top “Participants 5” button. Conversation must not require horizontal scrolling.

### Mobile: 767px and below

16px side gutters and a 56px header. Fixed bottom tabs: 「会話」, 「参加者」, 「自分のAI」; show one panel at a time. Keep day, phase, connection status, and perspective compactly visible above.

- Lobby tabs: 「待機室」, 「招待」, 「ルール」; put the start action in the lobby.
- Keep “Your AI” visible before phrase verification and lead to phrase entry. Explain “No AI registered” to spectators.
- Preserve that tab's location after death; update perspective and permissions within its panel.
- Account for `100dvh`, safe areas, and the software keyboard. Keep entry and send controls reachable.
- Preserve each panel's scroll position and unsent draft across tab changes. Sensitive drafts remain in memory only.
- No horizontal scrolling at 360px. Thirteen participants and long names must not break layout.

## 6. Detailed screen specifications

### 6.1 Home

Below the logo, show 「あなたのLLMを出場させよう！」. Keep setup, room creation, and joining near the top.

- 「ルーム作成」 and 「ルームを開く」 start collapsed; clicking their headings reveals the forms.
- 「AIのセットアップ」 contains a shared prompt to copy to the LLM and links to the player kit and SKILL. Send the seat-specific invitation after joining.
- Below: 「この端末で参加したルーム」. Each row includes name, RoomID, state, and updated time. Empty copy: 「まだ参加したルームはありません」.
- No global public room search in the initial version. A browser's recent-room history grants no authorization.

### 6.2 Creation form

One notebook page, ordered as your name, room name, AI count, rules, and participation mode.

- User name: 1–24 characters; room name: 1–48. Trim outer whitespace and agree on Unicode counting with the server. Names do not authenticate identity.
- Count: radio cards for 5 / 9 / 13; checkmark and border identify selection. Display the total of the server-supplied role distribution.
- Rules: presets with Japanese explanations. Prevent unsupported count/communication combinations. Default to five players and turn-based communication.
- Participation: default 「自分のAIも参加する」; also offer 「部屋を作って観戦する」. Spectator-only hosts do not reserve AI seats.
- Explain: 「AIが全員そろったら、あなたがゲームを開始できます。」
- Primary action 「ルームを作成」. Inline errors beneath fields. Preserve input on server rejection.
- Use an idempotency key so retrying creation does not duplicate rooms.

### 6.3 Joining a shared URL

Before joining, show only permitted public metadata: room name, rules, and available seats. Ask for a name and whether to bring an AI or spectate.

- Capacity counts AI seats, not spectators.
- Full-room copy: 「参加枠が埋まっています。観戦で入室できます」. No new AI seats after starting.
- Allow duplicate names with short disambiguating suffixes. Matching a display name never restores another person's seat.
- The server atomically reserves the seat. Preserve the entered name if another request fills the last seat first.

### 6.4 Lobby

Show room name, RoomID, and 「AI接続済み 3 / 5」. Each seat shows the human name, AI name, connection state, and “you” marker. Empty seats use dashed outlines.

Seat states: empty, waiting for AI, checking connection, connected, disconnected. Do not count a human's arrival as an AI connection.

Separate the two invitation actions clearly:

- 「友達を招待」: a Web URL containing no secrets, with brief copy confirmation.
- 「自分のAIを接続」: credentials for the owner's seat. 「AIへの案内をコピー」 packages the endpoint, RoomID, required protocol, and instructions for delivering the phrase privately. Even hosts cannot inspect other owners' invitation secrets.

If copying fails, show selectable text. In the connection guide, explain that RoomID alone does not make an ordinary chat AI join automatically.

Enable the host's 「ゲームを開始」 only when every AI seat is ready, rules are valid, and no start request is running. Otherwise show 「あと2体のAI接続を待っています」. Others see 「部屋主の開始を待っています」.

Freeze count changes and seat moves during start. On failure, display the reason and refresh seat state. Initially, count and rules are fixed after creation; use a new room to change them. Confirm leaving a waiting seat, then release it.

### 6.5 Keyphrase entry

When the game starts, continue displaying public conversation. Put a paper card with a key icon in the right panel; do not block the entire screen.

- Heading: 「自分のAIの視点をひらく」.
- Explanation: 「ゲーム開始後、あなたのAIが個別に伝えたキーフレーズを入力してください。」
- One pasteable input, masked by default, with a reveal control. Disable autocorrection and capitalization.
- Primary action: 「視点をひらく」. Invalid phrase: 「キーフレーズを確認してください」. For throttling, show when retry becomes available.
- On success, clear the phrase and refresh the session's authorized projection. Notify 「ハルの視点になりました」.
- For disconnected or unsupported AI, or a phrase not received, link to the connection guide. Never show another person's phrase or a host master phrase.

The server generates phrases bound to game, seat, and owner. A compatible AI client delivers its phrase through a private owner channel, such as its terminal or private chat. This requires client implementation. Never deliver it as a public game utterance. Initially support reload in the same device session, not cross-device ownership transfer. Name and RoomID alone cannot recover ownership.

### 6.6 Game and public conversation

The central transcript shows avatar, game name, utterance order, time, and body. Add a small “yours” marker to the owner's AI. Align all game speech left. Quotes link to the original utterance and show an excerpt.

- Toggle 「公開会話」 and 「出来事」. Add an authorized 「囁き」 tab for werewolf perspectives, clearly marked 「人狼だけ」.
- Use sticky-note separators for days and phases. Night changes the phase label to blue; the theme stays light.
- Provide day selection and speaker filtering. Search scope and counts include only authorized utterances.
- Render executions and attack outcomes at their public release time. Do not expose private ability activity or hidden-event counts to public viewers.
- Do not render raw speech HTML. Initially use plain text plus line breaks and wrap long URLs or unbroken strings.
- Normally show full messages. Over 500 characters, optional explicit 「続きを読む」 expansion must preserve reading position.
- Auto-follow only within 80px of the bottom. Otherwise preserve position and show 「新しい発言 N件 ↓」. Explicitly offer moving to the latest day when filtering by day.
- Deduplicate on reconnect and preserve position when prepending older history.
- No constant attention-seeking animation or per-utterance notification sounds.

### 6.7 Your AI panel

Order: perspective, role, private knowledge, consultation. Public viewers see an explanation of the lock instead of private information.

- Japanese role name plus a one-sentence explanation. Show werewolf teammates or delivered seer results only when that AI actually knows them.
- Display the actual result category, such as Human/Werewolf. Do not turn divination into an exact role reveal.
- Distinguish guesses, human advice, and AI explanations from server-confirmed results; use a label such as 「AIの見解」.
- Do not require hidden reasoning. Display short plans, explanations, and questions that the AI provides.
- Do not copy private results into the public timeline, even when those results mention other participants.

### 6.8 Private consultation

Always show the recipient: 「ハルにだけ届きます」. Paper background, pale mint human messages, white AI explanations. Keep this panel and its recipient distinct from public game speech.

- Input: 2–5 lines, proposed maximum 1000 characters. Enter inserts a newline; button or Ctrl/Cmd+Enter sends. IME composition confirmation must not send.
- Delivery: sending → server accepted → AI received. Also show failed, expired, and canceled due to death. “Received” does not mean adopted or applied.
- Only compatible AIs offer questions. Each question has an ID, options or free text, and a deadline. Display deadlines using the server clock offset; the server decides whether late answers are accepted.
- The game does not wait indefinitely for people. Explain 「回答がなければAIが判断します」.
- Messages have a submission ID, applicable phase, and expiry; retries cannot duplicate delivery. Restore drafts on failure and require explicit retry.
- Unsupported clients show a disabled input with 「このAIはWebからの相談に対応していません」. Gameplay connectivity does not imply consultation support.
- Initial scope excludes consulting other people's AIs, public human chat during play, and direct human voting controls.

### 6.9 Death and omniscient view

Upon the server's authoritative death event, disable consultation and expire undelivered advice, then obtain omniscient data. A client display toggle must never change authorization.

Notify: 「ハルが死亡しました。これ以降は神視点で観戦できます。」. Mark the owner's panel 「死亡・観戦中」 and keep 「神視点」 in the header. Indicate death using restrained gray plus text; no flashing or horror effects.

- Reveal all roles and private in-game events. Close the input; the owner's past consultation remains read-only.
- Offer public conversation, whispers, and all events. On permission changes, discard old projection caches and refetch history.
- Ordinary spectators do not gain omniscience when someone dies. Everyone receives it after game end.
- Do not allow rebinding to a living AI within that game. Remove unsent advice; never send it automatically.
- Do not transmit omniscient data to playing AIs. External calls or collusion through other accounts remain an operational limitation.

### 6.10 Finish, abort, and closure

Add a result card such as 「村人陣営の勝利」 above the conversation without clearing it. Show the owner's side's outcome, role list, and day-by-day history.

- Reveal game roles and in-game secrets, not human/AI consultations, phrases, or authentication data.
- Offer 「結果URLをコピー」 and 「同じ設定で新しいルーム」. Shared content must not include private consultation or secret tokens.
- Distinguish abort from victory; show an appropriate public reason such as 「接続エラーにより中断」 or 「サーバ再起動により中断」.
- Initial proposal treats abort as terminal and reveals game information. A room closed while waiting has no assigned roles.
- Display the server-provided retention period; do not promise indefinite storage without a decision.

## 7. Perspective and action matrix

| Viewer | Public information | Own role/knowledge | All roles/private game history | Consult own AI | Start |
| --- | --- | --- | --- | --- | --- |
| Joined, phrase not verified | Yes | No | No | No | Host in waiting room only |
| Living AI owner, verified | Yes | Yes | No | Compatible AI during play only | No |
| Dead AI owner, verified | Yes | Yes | Yes | No | No |
| Ordinary spectator | Yes | No | No | No | Host in waiting room only |
| Joined after finish/abort | Yes | Game information is public | Yes | No | No |

Room ownership and `viewMode` are separate axes. The server exclusively decides life state, phase, and permissions. Do not offer arbitrary AgentID-based perspective selection.

## 8. Proposed frontend data contract

These are proposed additions, not existing API declarations. Agree on types and errors first. Never pass raw `GameSnapshot` or `BroadcastPacket` directly to the browser; build a viewer-specific projection.

```ts
type RoomStatus = 'waiting' | 'starting' | 'running'
  | 'finished' | 'aborted' | 'closed';
type ViewMode = 'public' | 'agent' | 'omniscient';

interface RoomProjection {
  roomId: string;
  gameId?: string;
  status: RoomStatus;
  version: number;
  serverTime: string;
  viewer: {
    userId: string;
    isHost: boolean;
    ownSeatId?: string;
    viewMode: ViewMode;
    permissions: {
      canStart: boolean;
      canClaim: boolean;
      canConsult: boolean;
      canSeeWhispers: boolean;
    };
  };
  participants: ParticipantPublic[];
  game?: GameVisibleState;
  privateAgent?: AgentPrivateView;
  cursor: string;
}
```

Define supporting types during contract implementation. `ParticipantPublic` must omit unauthorized roles. `AgentPrivateView` contains only the owner's knowledge. Omit undisclosed fields at transmission; do not hide already-received values with CSS. Cursors are opaque so gaps cannot reveal the number of private events.

| Proposed API | Purpose |
| --- | --- |
| `GET /api/v1/room-presets` | Supported counts, rules, and consultation specifications |
| `POST /api/v1/rooms` | Creation and host session |
| `POST /api/v1/rooms/:id/join` | Name, participation/spectating, seat reservation |
| `GET /api/v1/rooms/:id` | Public pre-join metadata or viewer projection |
| `POST /api/v1/rooms/:id/agent-invitation` | Connection details for the owner's seat |
| `POST /api/v1/rooms/:id/start` | Host starts the game |
| `POST /api/v1/rooms/:id/claim` | Verify phrase and unlock perspective |
| `GET /api/v1/rooms/:id/history?cursor=...` | Paginated authorized history |
| `GET /api/v1/rooms/:id/events` | Lobby, perspective, and game SSE updates |
| `POST /api/v1/rooms/:id/consultations` | Advice or question response to own AI |
| `GET /api/v1/rooms/:id/consultations` | Own private consultation history |
| `POST /api/v1/rooms/:id/leave` | Release waiting seat and leave |
| `POST /api/v1/rooms/:id/close` | Host closes a waiting room |

No force-abort UI for running rooms initially. Separate Japanese display messages from machine-readable error codes. Suggested statuses: 409 for full room/duplicate start, 401 expired authentication, 403 insufficient permissions, 400 invalid input, 429 throttling, and 404 missing resource.

- UI uses same-origin port 8080 APIs and an HttpOnly session cookie, Secure under production HTTPS. Mutation endpoints require CSRF protection and Origin validation.
- Authenticate SSE using that session, not a phrase in the URL. Apply authorization during delivery, not just at connection time.
- Acquire a consistent snapshot/cursor pair, then subscribe from that cursor. Refetch on gaps; reconnect subscriptions when perspective or session changes.
- Separate public, owner-private, and consultation caches. Sensitive responses use `no-store`. No secrets in localStorage or shared caches.
- Protect or remove public access to legacy `/api/v1/games`, `/realtime`, TTS, and log routes to prevent bypasses.
- Design an authenticated separate AI control channel for consultation; do not insert it into normal gameplay WS responses. Negotiate client capabilities during handshake.

## 9. Components and implementation boundaries

Initial approach: Go-served HTML/CSS/JavaScript using ES modules. Node must not be required at runtime. SubAgents must not independently introduce different UI libraries. Maintain contract types in documentation; if TypeScript is adopted, agree on one build process.

| Component | Responsibility |
| --- | --- |
| AppShell / RoomHeader | Layout, room name, connection, perspective, phase |
| NotebookPanel / StickyLabel | Shared paper treatment and state labels |
| RaisedButton / Field / CopyField | Actions, input, clipboard feedback |
| RoomCreateForm / JoinForm | Validation, creation, entry |
| SeatList / SeatCard / InvitePanel | Seats, AI connection, invitation |
| PerspectiveGate / PerspectiveBadge | Phrase entry and current perspective |
| ParticipantList / RoleBadge | Participants and authorized roles |
| Timeline / TalkEntry / SystemEvent | Speech, events, days, follow behavior |
| PrivateKnowledgePanel | Owner-only role information |
| ConsultationPanel / QuestionCard | Private advice, questions, deadlines, delivery |
| ResultPanel / ConnectionBanner | Outcomes, abort, reconnect |
| MobileTabs / Dialog / Toast | Mobile navigation, confirmation, brief feedback |

Suggested files: CSS/JS/SVG/HTML under `web/static/`, served through Go embedding in `web/`. `transport/` handles serving and authentication, `orchestrator/` handles rooms/permissions/start, `service/` and `observer/` handle event projection and persistence, `store/` provides persistence, and `model/` holds view types. Preserve AGENTS.md dependency directions.

`logic` passes semantic events and read-only views through observers. Keep Web sessions, phrases, cookies, and HTML out of game logic. Make the boundary between secret internal events and public events explicit.

## 10. States, accessibility, and errors

- Loading: row skeletons or 「読み込み中」. Never briefly render placeholder secret roles.
- Empty conversation: 「ゲームが始まると、ここに会話が記録されます」. Distinguish connected from in progress.
- SSE disconnect: 「接続を確認しています。表示は更新されていません」. Keep last known information; recheck action permissions. Hold advice drafts rather than sending while connectivity is unknown.
- History failure: retain current history, show 「履歴を読み込めませんでした」 and 「再試行」.
- Session expiry: erase private views and consultations, return to entry. Do not leave old private DOM or caches when switching users.
- Focus headings after route navigation and return focus to dialog triggers on close. Incoming messages must not steal focus.
- Associate labels, hints, and errors with fields; set `aria-invalid`. Radios and tabs must be keyboard-operable.
- Use `aria-live=polite` for important connectivity, delivery, and perspective changes. Announce new-message counts instead of repeatedly reading the full transcript.
- Target 4.5:1 for normal text and 3:1 for control boundaries/focus. Measure actual combinations during implementation.
- Do not rely on color alone. Check 200% zoom, keyboard use, safe areas, OS dark preference, and reduced motion.
- An opaque cover is not access control. Verify no hidden roles in text, accessibility trees, Network responses, or DOM attributes.

## 11. SubAgent work split

Agree on tokens, mock projections, API contracts, and state names before parallel work. Each agent owns its assigned files; do not duplicate shared CSS or API definitions independently.

| Owner | Scope | Deliverable |
| --- | --- | --- |
| A: Foundation/design | Tokens, shared controls, AppShell, responsive behavior | Specimens of button states, paper, forms, tabs |
| B: Rooms/entry | Home, creation, joining, lobby, invitations, start | Empty/full/disconnected/starting room states |
| C: Viewing/AI | Transcript, private data, phrase entry, consultation, death, results | Every perspective and reconnect state |
| D: Backend | Rooms, sessions, AI integration, authorization, SSE, persistence | Agreed APIs and integration tests with real clients |
| Integrator | Contract alignment, CSS/state integration, overall QA | A complete game through the Web experience |

Order: A and D establish contracts and examples → B/C implement against mocks → connect real APIs → verify authorization and gameplay together → update bilingual documentation and Docker.

Clearly label mocks 「デモ」. Prepare deterministic fixtures for public, living seer, living werewolf, deceased owner, finished game, disconnect, and thirteen participants. Do not ship a production query parameter that overrides viewMode for demos.

## 12. Acceptance criteria

### Appearance

- Inspect major screens at 1440×900, 1024×768, 390×844, and 360×800; retain screenshots.
- Consistent paper, grid, sticky notes, and tactile buttons; long conversation remains on plain readable surfaces.
- Check rest, hover, active, focus, disabled, and loading states. Pressing controls must not shift neighboring layout.
- OS dark preference, night phases, omniscient view, and results remain Light.
- Long Japanese names, URLs, thirteen seats, and utterances over 500 characters do not overflow horizontally.

### User flow and information control

- Two independent sessions view the same game through different owned AIs; a third spectator receives only public data.
- Duplicate names, another person's phrase, guessed RoomIDs, and edited URLs do not grant another perspective.
- After verification, displayed knowledge matches what the AI receives, including teammate knowledge and result release timing.
- The server enforces death → consultation stop/pending cancellation → omniscient access, including races with sending.
- Spectators and hosts do not gain omniscience from another participant's death.
- No secrets leak through legacy APIs, SSE recovery, history, counts, DOM attributes, logs, or TTS.
- Reload/reconnect does not lose or duplicate conversation and preserves the position of readers browsing earlier messages.
- Reopening a finished room shows results; private consultations and phrases remain private.
- Verify unsupported consultation clients, expired messages, failed delivery, and aborted games.

### Implementation verification

Run existing `go build ./...`, `go test -race ./...`, `go vet ./...`, and formatting checks. Focus added tests on behavior: room isolation, seat races, authorization, phrases, death/send races, and SSE recovery. Exercise the UI using a keyboard and the viewport sizes above. Update all distributed configurations, Docker, and Japanese/English API/config/protocol documentation for configuration, port, and AI communication changes.

## 13. Short handoff prompt

> Read AGENTS.md and doc/en/web-ui-design.md and implement your assigned scope. Use Web 8080 / AI WS 8081, Light only, notebook paper, sticky notes, and raised buttons that sink when pressed. Cover home, creation, joining, lobby, gameplay, death, and results. Render only server-projected information; never send other living players' secret roles or knowledge to the browser. Separate host permissions from perspective permissions and private consultation from public conversation. Label mocks and do not report unimplemented APIs as complete. Reuse shared tokens and contracts, verify desktop/mobile, long text, disconnects, errors, and keyboard interaction, and report changes, validation, and remaining work.
