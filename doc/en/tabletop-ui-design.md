# AI人狼バトル！ Tabletop Spectator UI Design v1

[日本語](../ja/tabletop-ui-design.md) · [Existing Web design](web-ui-design.md)

2026-09-28. **Initial implementation available.** This document supersedes the match screen, history, and spectator effects sections of the existing Web design. Home, joining, invitations, Light-only styling, and owner perspective authorization remain applicable.

## 1. Direction

Send your robot into the match and watch from the same table. The table and players become the primary screen; the long transcript moves into a notebook opened on demand.

The reference contributes inward-facing seats, attention to the speaker, and participation through a stable seating arrangement. Its environment and characters are not reproduced. Use the existing logo's mint, coral, yellow, paper surfaces, and raised buttons. Humans unlock their perspective, read history, and advise their AI; the AI still speaks and votes.

![Desktop and mobile concept](../design/tabletop-ui-v1/concept.png)

The generated image establishes color, texture, and hierarchy, not final dimensions or copy. It illustrates five seats, four survivors, seat 03's latest speech, and seat 01's owner perspective. Signs, furniture, and decorative copy are optional and should be removed first on narrow screens. The generated logo is illustrative; implementation must use the existing logo asset unchanged.

## 2. Information that remains visible

1. Day and phase; show a generic in-progress state when phase data is unavailable.
2. Living AI count, separate from connection count.
3. Your seat and authorized viewing perspective.
4. Who last spoke and what they said.
5. Whose response is awaited when the server can report it. Do not invent the next speaker.

Priority: table, latest speech, perspective/progress, private knowledge, then history. Move RoomID, sharing, rules, and leaving into a menu during the match.

## 3. Seats and avatars

- Small robot pieces face the center: front-facing at the far side, angled at the sides, and a rear view for the owner's near-side piece. Numbers and names remain upright.
- Colors and shells are neutral seat identities. Do not encode roles with wolf ears, wands, or shields. In omniscient mode, show roles in separate badges while preserving each seat's appearance.
- Keep seat numbers fixed. Use `agent_idx` for the game number and `seat_id` for identity. Rotate the visual arrangement so the owner's seat is near the bottom, preserving clockwise order. Spectators share a common orientation.
- The bottom seat is not necessarily 01: owner 07 stays numbered 07. Do not compact or reorder seats after death or reconnection.
- Show number, short AI name, and status. Only the owner's seat gets “あなたのAI”. Truncate long names and expose full AI and human names in seat details.
- Tapping a seat opens details, not a vote or ability action. Unauthorized roles remain “非公開”.

| State | Appearance |
| --- | --- |
| Alive | Normal piece and legible name |
| Awaiting a public response | Outline ring and “応答待ち”, with a restrained pulse |
| Latest speaker | Speech symbol and brief pop; number matches the current-speech card |
| Dead | Remains in its seat, desaturated piece and “死亡” ribbon; retain name contrast |
| Connection unknown | A separate connection label only when supported by reliable data; never reuse the death treatment |

The awaited responder and latest speaker can be different players. Keeping a speech card visible must not imply that its author is currently thinking.

## 4. Layout

### Desktop: 1200px and wider

- Maximum width 1440px, 64px header, approximately 48px progress bar. Table and current speech form the main area; a 280–300px owner panel sits beside it.
- Table height approximately 420–540px with flexible width, a small central motif, and seats around its perimeter. No permanently visible transcript.
- Current-speech card below, approximately 120–160px tall, showing speaker number/name, text, and a full-text action. Allow vertical page scrolling when larger text needs more space.
- Owner panel contains role, private knowledge, and consultation entry. Before unlocking, show the key form; spectators see perspective and rule information.
- Keep “履歴をひらく” available below. Open history in an independent drawer instead of squeezing history and the owner panel beside the board.

### Tablet: 768–1199px

Use a single main column for table and speech, with the owner panel behind a button. Preserve the same information for five, nine, and thirteen seats instead of compressing it into a narrow sidebar.

### Mobile: 360–767px

- Start from `100dvh`: 56px header, approximately 44px progress row, 300–400px table, 112–160px current speech, and 56px bottom actions plus safe area. These are targets, not rigid totals. Short screens reduce decoration and allow vertical scrolling.
- Compact logo and perspective at the top; room name may truncate to one line. Day, phase, and living count remain visible below.
- Main bottom actions are “履歴” and “自分のAI”. Seat taps open participant details; a menu provides the full list.
- Latest speech must clear the fixed navigation. Body text is at least 16px and touch targets at least 44×44px.
- When the keyboard opens, prioritize the consultation form rather than maintaining table height. Preserve drafts and focus.

### Layout by player count

| Players | Desktop | Mobile |
| --- | --- | --- |
| 5 | Oval with one near seat and two on each side | Vertical oval, large pieces and names |
| 9 | Oval with moderately smaller pieces | One near seat plus four on each side, preserving order |
| 13 | Wide oval; bounded names with details on tap | One near seat plus six on each side of a long table; rows at least 48–52px, touch targets at least 44px even with smaller art |

Do not scale the entire five-player layout down for nine or thirteen players. Reduce center decoration and shorten names while prioritizing numbers and status. Keep every seat present without horizontal swiping. At 360×640 or 200% text size, allow vertical page scrolling rather than forcing everything into one viewport.

## 5. Current speech and history

### Current-speech card

- The main board shows one latest public utterance. Authorized whispers appear only after explicitly switching to the wolf-only channel. Private owner consultations never enter this card.
- Display received text together. Do not use typing effects or audio waveforms that imply unsupported streaming or sound playback.
- Collapse long speech after roughly three or four lines and open that exact event in history with “全文を読む”. Preserve the complete utterance.
- Do not replace expanded text while it is being read. History retains its position while the board continues to update.
- “Awaiting seat 05” and “Latest speech from seat 03” are separate labels. Hide seconds unless a reliable deadline exists.

### History notebook

- Closed initially. Desktop opens a 420–480px right drawer; mobile opens a bottom sheet up to approximately 88dvh, expandable for full reading.
- Provide a heading, close action, day and speaker filters, public speech/events, and authorized whispers. First opening starts at the latest entry; later openings restore reading position.
- Do not jump when new speech arrives while reading older entries. Show a new-speech count and a return-to-latest control. Count only speech visible to that viewer.
- Follow automatically only at the end. Closing returns to a board showing the current seats and phase.
- Opening history does not pause the game. This is simultaneous live viewing and history reading, not recorded replay.
- Make the background inert and retain focus inside the sheet. Escape and an explicit close button return focus to the opener; gestures are not the only exit.

## 6. Animation specification

Use HTML/CSS and lightweight avatar artwork. No initial requirement for WebGL, physics, or video backgrounds. Game progress never depends on animation completion.

| Trigger | Effect | Target duration |
| --- | --- | --- |
| Match begins | Small landing motion for seats and progress bar entrance | Under 600ms total |
| Public response wait | Seat ring; no constant jumping | 180ms entrance, subtle 1.6s pulse |
| Public speech accepted | Piece bounces 2–4px and speech card changes | 220–300ms |
| Phase change | Sticky label changes; yellow day, pale lavender night | 300–450ms |
| Voting | Show chips/results only when permitted to be public | About 300ms |
| Death | Piece settles and gains a death ribbon | About 450ms |
| Owner's AI dies | Apply server-authorized omniscient badge and knowledge | About 300ms |
| Match ends | Result card and one small paper-confetti effect | Confetti under one second |

- Night remains Light. Never reveal hidden divination targets with beams or animate only the wolves during private actions.
- Store all visible events in history but coalesce rapid decorative effects toward the latest state. Do not replay dozens of old bounces. Always apply final death and result state.
- Reconnection applies the latest snapshot immediately without replaying past effects. Animate each event identity at most once.
- Pause decorative motion in hidden tabs and synchronize on return. With `prefers-reduced-motion`, remove movement, pulses, and confetti while keeping text and outlines.
- Notifications and speech audio are outside this scope. A speaking robot effect does not claim actual audio playback.

## 7. Baseline investigation and required extensions

| Display | Current support and implementation |
| --- | --- |
| Seats, owner, living count, perspective | Use `seats` and `viewer` from `GET /rooms/:id`; derive living count from `alive` |
| Latest speech and disclosed events | Use history fields such as `seq/type/from_idx/text/at`; highlighting received speech is supported |
| Exact day/night phase | No precise room progress projection exists. `room/gameRecorder.OnPhase` currently only logs, and turn-based boundaries are not fully reported |
| Awaited AI and remaining seconds | Missing from the Web projection; never infer them from the last `talk` event |
| Next speaker/order | `logic/communication_turn.go` shuffles order and skips ineligible speakers. Do not connect seat order with speaking-order arrows |
| In-match connectivity | `seats.connected` does not mean alive or currently responsive. Do not animate a disconnection without reliable new status data |

Label preliminary static fixtures as demos. Shipping a current responder indicator requires the server work below. Otherwise, honestly show only the latest received speech.

### Progress projection

Add public `phase`, progress `revision`, and `active_public_turn` under `RoomView.progress`. The latter exists only while awaiting public TALK and contains `turn_id / agent_idx / state / deadline_at`. The deadline is optional and null when no authoritative value is available. Correct client clock offset with `server_time`.

- Public phase vocabulary includes `day_discussion / day_vote / night / finished`. Do not subdivide public night into the actual sequence of divination, guarding, and attacks.
- Add a next-speaker value only when the scheduler can report its real plan. Initially, unknown is acceptable; do not invent order from seat numbers or living count. Freeform mode has no single turn order.
- Existing `observer.OnRequest/OnResponse` callbacks contain raw traffic and secrets. Do not forward them to browsers. Add read-only `model` views and semantic observer notifications where accurate turn IDs, deadlines, and public phase boundaries require them.
- `logic` reports actual phase boundaries and request start/completion/failure through observers. `room` projects this per viewer, and `transport` delivers it. Do not put HTTP or DOM responsibilities into `logic`.
- Do not treat NAME probes as speech turns or pre-send notifications as successful delivery. Distinguish the response deadline from probe/grace time and change to a checking state after expiry.
- Never expose hidden night actors, targets, per-agent deadlines, or completion counts publicly. Omniscient viewing does not grant access to someone else's private owner messages.

### Synchronization and disclosure timing

Retain the named `room` SSE channel, history cursor, reconnect recovery, and ten-second fallback checks. Give progress-only updates their own revision so awaited turns update even when no speech sequence is added.

Advancing the day can reveal previously hidden votes. Re-project history on day/disclosure changes as well as death, key unlock, and finish. Merely appending events with `seq > cursor` misses earlier events that become visible later.

Server seat, life, and disclosure state is authoritative. Apply current state even during effects. On disconnection, show waiting-for-updates and stop countdown/wait animations. A local timer reaching zero must not declare death, disqualification, or the next turn.

## 8. Perspective and advice

- Locked: public table and a key-unlock entry. Seat numbers or matching user names never unlock secrets.
- Owner: role and results belong in the personal panel. Wolf whispers appear only in authorized views.
- Death: keep the seat. Reveal roles only after the server authorizes omniscient mode, and disable advice. Spectators do not gain omniscience when another player's AI dies.
- Finish: keep the table and show results, history, and new-room actions. Do not keep displaying an in-progress reconnect badge.
- Keep key phrases, invitation tokens, and other owners' private messages out of images, public DOM, and public streams. The concept's seer label is exclusively an owner-panel example.

## 9. Components and artwork

| Component | Responsibility |
| --- | --- |
| GameStage / TableLayout | Five/nine/thirteen-seat layouts, orientation, responsive behavior |
| PlayerSeat / RobotAvatar | Fixed number, ownership, life, public response wait, details |
| ProgressBar | Day, phase, living count, perspective, connection |
| CurrentSpeech | Latest permitted speech, author, full-text entry |
| HistoryDrawer | Authorized history, filters, position, unread count |
| OwnAgentSheet | Key entry, personal knowledge, private consultation |
| StageEffects | Once-only effects, catch-up, reduced motion |

Do not implement the whole interface as a single generated image. Separate table art, robots, and shadows; render text, counts, controls, and state in the DOM. Continue with existing ES modules and Go embedded delivery without requiring a new runtime.

Artwork includes neutral robot front/angled/rear views, color variants, a table surface, and minimal props. Start with static assets and CSS; frame-by-frame sprites are a later enhancement. Assign colors independently of roles. Use small SVG icons, existing Light tokens, and supplemental logo-derived coral and dark plum. Prioritize body-text readability.

## 10. Implementation order and subagent assignments

1. **Contract and fixtures:** define five/nine/thirteen-player, day/night, waiting, death, and finish fixtures with viewer projections. Demo paths cannot override production perspective permissions.
2. **Static table:** build stage, seating, current speech, and history sheet. Validate long text and small screens here.
3. **Accurate progress:** implement observer events and room projections, then connect public TALK waits and phases to real data.
4. **Light effects:** add speech, death, day/night, and finish transitions; skip historical effects on reconnect.
5. **Integration:** connect private information, history, and advice; validate permissions, mobile use, and long matches.

| Owner | File responsibility | Deliverable |
| --- | --- | --- |
| A: visuals and layout | Stage/seat CSS and artwork | Three player counts on desktop and mobile |
| B: progress and projection | model/observer/logic/room/transport | Public phase and response waits with a filtered contract |
| C: speech and history | CurrentSpeech/HistoryDrawer/OwnAgentSheet | Long speech, past reading, unread state, keys and advice |
| Integrator | Application state, effects, existing-screen integration | API sync, permissions, visual checks, bilingual docs |

Use shared fixtures and contracts. Agree on component file boundaries before concurrently rewriting `app.js` or `style.css`. A visual-design subagent must not invent speaking order, deadlines, or disclosure rules.

## 11. Acceptance criteria

- At 1440×900, 1024×768, 390×844, 360×800, and 360×640, five/nine/thirteen seats do not overlap or require horizontal scrolling. Small screens and 200% text retain controls.
- Speaker, response wait, owner, and death are distinguishable without color. Speaker number matches current speech.
- Seats, history, close, and personal panels are keyboard accessible. New events never steal focus; reduced motion loses no information.
- History reading does not stop live updates. New messages preserve reading position, mobile panels, and drafts.
- Vote disclosure, owner death, finish, and reconnection do not omit or duplicate information or replay old effects.
- Inspect public/owner/omniscient traffic and DOM for hidden night actors, targets, and private messages.
- During implementation, run Go and browser checks appropriate to the changes. Docker verification may remain omitted as requested by the user.

## 12. Image-generation record

Created with built-in ImageGen using the existing logo and the user's seating-reference image. The [saved concept](../design/tabletop-ui-v1/concept.png) and [exact prompt](../design/tabletop-ui-v1/prompt.txt) are included. This is a design-review image, not a finished in-game asset.

## 13. Initial implementation

- `web/static/tabletop.js` / `tabletop.css`: neutral robots and the table. Lightweight SVG and CSS draw individual seats, labels, and states instead of using the mockup as a background image.
- `web/static/app.js` / `match.css`: match screen, latest speech, owner panel, participant details, and results. Lobby, invitations, and home retain their existing navigation.
- `web/static/history.js`: a history notebook using native `dialog`. Day, speaker, and channel filters preserve reading position and unread counts while reading older entries. Private messages stay in the owner panel.
- The server supplies actual public phases and public response waits through `progress`. Deadlines are `null` in this release; no countdown or next speaker is displayed.
- Asset versions include the split modules so a screen update does not retain outdated components.

The concept image remains a visual reference, not a screenshot of the implementation. Audio, WebGL, and frame-by-frame character animation are outside the initial release.

### Implementation checks

Five, nine, and thirteen seats were checked at 1440×900, 1024×768, 390×844, and 360×640; detected seat overlaps and horizontal overflow were corrected. The omniscient view with role badges was also checked at 1280px, and controls remain usable at 360px with 200% text size. Below 901px, seats use vertical side columns; vertical space expands for larger text and role badges.

History filters are collapsible. Browser checks covered reading position, unread counts, drafts, Escape focus restoration, keyphrase entry, night, death, and game completion. Public progress passed the full Go test suite; death permissions passed `go test -race ./room` and the real-match `TestRoomGameEndToEnd`. `go build ./...` and `go vet ./...` also passed. Run `node --test web/test/tabletop.test.mjs` to verify stable seating, public speakers, name escaping, and role-independent seat colors. Docker verification was omitted.
