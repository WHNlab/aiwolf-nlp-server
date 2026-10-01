# About the REST API

[api in Japanese](/doc/ja/api.md)

In addition to the WebSocket endpoint for agents (`/ws`), the server provides a read-only HTTP API for health monitoring and spectating games.
There are no endpoints for updates such as starting/stopping games or submitting configurations.

All responses include `Access-Control-Allow-Origin: *`.

## Endpoints

| Endpoint | Authentication | Description |
| --- | --- | --- |
| `GET /api/v1/healthz` | Not required | Liveness check |
| `GET /api/v1/readyz` | Not required | Whether new connections are accepted |
| `GET /api/v1/ruleset` | Not required | The ruleset this server is running |
| `GET /api/v1/games` | Required | List of games in progress (not published when `server.web.enable` is `true`) |
| `GET /api/v1/games/{id}` | Required | Current state of the specified game (same as above) |
| `GET /api/v1/games/{id}/events` | Required | Event stream for the specified game (SSE) (same as above) |
| `GET /realtime/...` | Required | Static delivery of realtime broadcaster logs |
| `GET /tts/...` | Not required | Static delivery of TTS segments |

Endpoints marked as "Required" demand a token only when `server.authentication.enable` is `true` in the configuration file.
`/realtime` is exposed only when `realtime_broadcaster.enable` is `true`, and `/tts` only when `tts_broadcaster.enable` is `true`.

## Authentication

When `server.authentication.enable` is `true`, the spectator endpoints require a RECEIVER token.
The token is an HMAC-signed JWT using the `SECRET_KEY` environment variable as the secret key, and its claims must include `role: RECEIVER`.
(The token used when an agent connects to `/ws` is a different one, containing `role: PLAYER` and a `team` claim indicating the team name.)

Pass it in either of the following ways.

```bash
curl "http://127.0.0.1:8080/api/v1/games?token=<TOKEN>"
curl -H "Authorization: Bearer <TOKEN>" http://127.0.0.1:8080/api/v1/games
```

If the token is missing or invalid, `401 Unauthorized` is returned.

## Room API (Web UI)

When `server.web.enable` is `true`, a separate HTTP server for the Web UI (`server.web.host` / `server.web.port`, default 8080) starts and exposes the following room APIs.+Human viewers are identified by an HttpOnly session cookie (`aiwolf_session`), and roles and private exchanges are filtered per session according to their viewing permission.+Mutating endpoints apply a lightweight CSRF check using the Origin header.

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/room-presets` | List of supported player counts, role compositions, and communication modes |
| `GET /api/v1/rooms?status=active|finished&q=query` | List public rooms. `active` (default) includes waiting, starting, and running rooms; `finished` includes completed and aborted replays. Newest first, up to 50 |
| `POST /api/v1/rooms` | Create a room (body: `room_name`, `user_name`, `agent_count`, `mode`, `is_public`, `agent_source`, `rental_agents: [{name, skill}]`; legacy `rental_name` / `rental_skill` remain supported; public by default) |
| `POST /api/v1/rooms/{id}/join` | Join a room (body: `name`, `mode`, `agent_source`, `rental_name`, `rental_skill`; mode=participate reserves a seat) |
| `POST /api/v1/rooms/{id}/leave` | Leave while the room is waiting |
| `POST /api/v1/rooms/{id}/close` | The host closes the room |
| `POST /api/v1/rooms/{id}/start` | The host starts the game (all seats must have an agent connected) |
| `POST /api/v1/rooms/{id}/claim` | Legacy key phrase verification (body: `phrase`; not needed in the Web UI) |
| `POST /api/v1/rooms/{id}/consultations` | Send advice to your own agent (body: `text`; only while your seat is alive) |
| `GET /api/v1/rooms/{id}/consultations` | List the private exchanges between you and your agent |
| `GET /api/v1/rooms/{id}/invite` | Returns the agent connection URL (`room_id` + `seat_token`) and guide text for your seat |
| `GET /api/v1/rooms/{id}` | Current room state; roles are masked according to the viewer's perspective |
| `GET /api/v1/rooms/{id}/history?cursor=N` | Event history filtered by viewing permission (seq > cursor) |
| `GET /api/v1/rooms/{id}/events` | Event stream (SSE, `room` update notices and `heartbeat` keepalives) |
| `PUT /api/v1/rooms/{id}/rental` | Update your rental seat's AI name and custom skill while waiting (body: `rental_name`, `rental_skill`) |
| `PUT /api/v1/rooms/{id}/rentals/{seat_id}` | Update or reconnect your own or an additional rental seat you created while waiting |
| `GET /api/v1/rental-capabilities` | Rental AI availability, remaining concurrent capacity (`available_slots`), supported player counts, skill length limit, and the user-facing reason when disabled |

The `progress` field in `GET /api/v1/rooms/{id}` contains public game progress. `phase` is one of `waiting`, `day_discussion`, `day_vote`, `night`, or `finished`; `revision` increments whenever progress changes. `active_public_turn` is set only while a sequential daytime public TALK response is pending and is `null` otherwise. Its value contains `turn_id`, `agent_idx`, `state: "waiting"`, and `deadline_at: null`. The server has no authoritative deadline to project. Freeform chat and night actors or secret subphases are not exposed as a single pending turn.

SSE sends a `room` notice when progress changes, even if the history `seq` does not. When the visibility of existing history changes, such as votes revealed on the following day, refetch `GET /history` without a cursor after a day, status, or viewpoint update. A cursor only returns events with a newer seq.

The viewpoint (`viewer.view_mode`) is one of `public` / `agent` / `omniscient`.+After the game ends or is aborted, and for viewers whose own agent has died, the viewpoint becomes `omniscient` and all information including roles and attack votes is visible.

After a game starts, a seat owner sees their own agent's role and private information automatically through the session cookie created on joining. Spectators remain in the public view. Rooms created with `is_public: false` are omitted from listings and search but can be opened by anyone who knows the RoomID. After the game, both public and private rooms can be viewed by RoomID from the omniscient perspective.

Completed and aborted matches retain publicly viewable events and seat information as JSON for 30 days. Private consultations, connection tokens, key phrases, and cookies are never archived. Match pages and histories survive a restart; running matches and private consultations do not. Set `AIWOLF_REPLAY_DIR` to choose the storage path (default `./data/room-replays`). Docker Compose mounts a persistent volume at `/data`. Expired files are removed at startup or when rooms are accessed.

When execution or attack confirms a death, the seat status, owner perspective, and advice permission update with that event. A successfully guarded agent remains alive.

## Endpoint Details

### GET /api/v1/healthz

Indicates that the process is able to respond. Always returns `200 OK`.

```json
{ "status": "ok", "version": "v0.0.0" }
```

### GET /api/v1/readyz

Indicates whether new connections can be accepted.
While shutting down after receiving `SIGTERM` or similar, it returns `503 Service Unavailable` with `{"status": "draining"}`.

```json
{ "status": "ready" }
```

### GET /api/v1/ruleset

Returns the ruleset this process is running. Since the server runs with one configuration per process, it returns a single ruleset rather than a list.

- `agent_count` (int): The number of agents per game.
- `max_day` (int): The maximum number of days in the game. -1 if there is no limit.
- `vote_visibility` (bool): Whether the results of votes are revealed.
- `is_optimize` (bool): Whether optimized combination matching is enabled.
- `self_match` (bool): Whether self-play mode is enabled.
- `roles` (dict[str, int]): The number of agents for each role.

### GET /api/v1/games

Returns the list of games in progress. Finished games are removed from the registry and are therefore not included.

```json
{ "games": [ { "id": "...", "agents": [], "finished": false } ] }
```

Each element has the same structure as [GET /api/v1/games/{id}](#get-apiv1gamesid), except that `day` and `status_by_agent` are not populated.
Use the individual endpoint if you need them.

### GET /api/v1/games/{id}

Returns the current state of the specified game. If it does not exist, `404 Not Found` is returned.

- `id` (str): The identifier of the game.
- `day` (int): The current day.
- `finished` (bool): Whether the game has finished.
- `win_side` (str): The winning team. Normally an empty string, since finished games are removed from the registry.
- `agents` (list[Agent]): The list of participating agents, as of the start of the game.
  - `idx` (int): The index of the agent.
  - `team_name` (str): The team name.
  - `original_name` (str): The agent name given at connection time.
  - `game_name` (str): The name of the agent in the game.
  - `role` (str): The role.
  - `alive` (bool): Whether the agent is alive. Always `true`, since it is the value at the start of the game.
- `status_by_agent` (dict[int, str]): The status (`ALIVE` / `DEAD`) for each agent index.

Refer to `status_by_agent` rather than `agents[].alive` for the current status.

### GET /api/v1/games/{id}/events

Streams the events of the specified game via Server-Sent Events. If the game does not exist, `404 Not Found` is returned.
The event name is `broadcast`, and the data is the JSON of a [broadcast packet](#broadcast-packet).

The most recent packet is delivered immediately upon subscribing, so a late subscriber can still obtain the current state.
The stream ends when the game finishes.

```bash
curl -N http://127.0.0.1:8080/api/v1/games/<GAME_ID>/events
```

```text
event:broadcast
data:{"id":"...","idx":1,"day":0,"is_day":true,"agents":[...],"event":"開始","message":"ゲームが開始されました","timestamp":1750000000}
```

> [!NOTE]
> The same content is also recorded to a JSONL file by `realtime_broadcaster` and served statically from `/realtime`.
> SSE can be used instead of polling that file.

## Broadcast Packet

- `id` (str): The identifier of the game.
- `idx` (int): The packet index, numbered sequentially from 1 within a game.
- `day` (int): The current day.
- `is_day` (bool): Whether it is the day section.
- `agents` (list[Agent]): The current information of each agent.
  - `idx` (int): The index of the agent.
  - `team` (str): The team name.
  - `name` (str): The name of the agent in the game.
  - `profile` (str | None): The profile.
  - `avatar` (str | None): The URL of the avatar image.
  - `role` (str): The role.
  - `is_alive` (bool): Whether the agent is alive.
- `event` (str): The type of the event.
- `message` (str | None): The text associated with the event, or the content of a speech.
- `from_idx` (int | None): The index of the agent that acted.
- `to_idx` (int | None): The index of the agent that was targeted.
- `bubble_idx` (int | None): The index of the agent whose speech bubble should be displayed.
- `timestamp` (int): The time the event occurred (Unix seconds).

The event types are as follows. Note that the `event` values are Japanese strings.

| event | message | from_idx | to_idx | bubble_idx |
| --- | --- | --- | --- | --- |
| `開始` (start) | Fixed text | - | - | - |
| `終了` (end) | Winning team | - | - | - |
| `トーク` (talk) | Content of the speech | - | - | Speaker |
| `囁き` (whisper) | Content of the speech | - | - | Speaker |
| `投票` (vote) | - | Voter | Vote target | - |
| `襲撃投票` (attack vote) | - | Voter | Vote target | - |
| `追放` (execution) | - | - | Executed agent (omitted if none) | - |
| `占い` (divine) | - | Seer | Divine target | - |
| `護衛` (guard) | - | Bodyguard | Guard target | - |
| `襲撃` (attack) | - | -1 if guarded | Attack target (omitted if none) | - |

## Player Kit and Invitation Settings

`GET /api/v1/rooms/{id}/invite` returns `ws_url`, `mode` (`turn` / `freeform`), `kit_version`, `kit_path`, and `guide_text` only to the seat owner. Use `?download=1` to save it as `invite.json`. Responses use `Cache-Control: no-store`.
The public URL prefers `server.web_socket.public_url`; it is not inferred from the web domain or TLS termination.
`GET /downloads/aiwolf-player-0.2.1.zip` serves the CLI and SKILL without credentials or authentication. `GET /downloads/aiwolf-player.zip` serves the current version. `GET /agent/SKILL.md` and `GET /agent/GUIDE.md` serve the instructions. See the [player kit guide](/doc/en/agent-kit.md).

## Rental AI

Seats can run an agent hosted by the server's OpenAI API key. Players only enter an AI name and a custom skill of up to 200 characters in the browser — no API key or CLI is needed.

- When creating a room, pass `agent_source: "rental"` and `rental_agents: [{"name":"シオン","skill":"Reason carefully"}, ...]`. The first entry becomes the creator's own seat; the rest are additional seats configured by the creator. The legacy single-seat creation fields and single-seat join fields remain supported. The invite endpoint returns nothing for rental seats; connection tokens stay inside the server.
- Only 5-player turn-based rooms are supported, with 1–5 rentals per room and five concurrent workers in total. Friends can occupy unreserved seats. A user cannot create or join multiple rental rooms concurrently.
- The creator can inspect and retry setup failures for additional seats only before the match starts. Their roles, key phrases, private messages, and in-game failure details remain hidden during play.
- The owner's seat perspective opens automatically using the room session; no key phrase entry is needed.
- `rental_name` is the team/bot name and must contain 1–6 Unicode code points. Blank defaults to `レンタルAI`. Surrounding whitespace is trimmed; control characters and `Over`, `Skip`, and `None` are rejected. External agents also use their NAME response as their bot name. Names are shared across games, voting targets, and records; collisions receive a numeric suffix within six characters.
- Both the latest speech and history display full text. Rental agents receive a generation limit calculated from the per-talk cap, base allowance, and remaining allowance. Oversized output is shortened at a complete sentence; if none fits, an ellipsis is added. The owner receives a note when speech is shortened. Text already discarded in historical records cannot be restored.
- Before connecting, the server verifies that the model returns a structured response (up to 35 seconds, with success or failure shared for 60 seconds). Failed seats have `rental_state: "failed"` and do not count as ready. During setup, only the seat owner (or the room creator for an additional seat) receives `rental_error`. After resolving the cause, retry through that seat's update endpoint; during the cache period it returns the previous result.
- Generation failures during games also populate `rental_error`, cleared on recovery. Quota, authentication, and other permanent errors immediately set `degraded` without retrying. Only transient 429/5xx responses get one delayed retry. Three consecutive failures or a usage limit also set `degraded` and stop API calls for that match. Fallbacks end speech and automatically choose a legal target.
- Readiness checks and retries count toward budgets. Explicit 4xx rejections do not count as generation cost. Failure to read or persist usage stops new requests.
- When `OPENAI_API_KEY` is unset, or the daily budget `AIWOLF_RENTAL_DAILY_USD` is unset, zero, or exhausted, the feature reports itself unavailable. Usage is stored in `rental-usage.json` under `AIWOLF_DATA_DIR` (default `./data`).
