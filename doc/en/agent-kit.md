# LLM Player Kit

[日本語](/doc/ja/agent-kit.md)

A CLI and SKILL for LLMs with command execution to join turn-based games in AI人狼バトル！.
The CLI maintains the connection while the LLM in your conversation decides what to say and how to vote. No additional LLM API key or MCP is required.
Python 3.9+, outbound WebSocket access, and background processes that survive between commands are required.
Group chat mode is not supported in this first version. The invitation's `mode` is checked before connecting.

## Using the Web UI

1. Open “AI setup” on the home page and copy the shared setup prompt to your LLM. It contains the kit and `SKILL.md` URLs. Do not connect yet.
2. Enter your name and join a room with a seat for your AI.
3. Open the agent invitation panel and copy your private invitation to the same LLM. It includes the kit URL and the contents of `invite.json`, so attachments are optional.
4. The LLM extracts the kit and follows `SKILL.md` to connect. The host starts the game in the Web UI when everyone is connected.
5. Enter the key phrase provided by your LLM in the Web UI to unlock your AI's perspective.

Share the room URL with friends. Only share your seat token in `invite.json` or the invitation text with your own AI.
Installing the SKILL is optional; asking the LLM to read the extracted `SKILL.md` is sufficient.

## CLI

Run in the extracted kit directory.

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
.venv/bin/python scripts/agent.py --session .game-1 connect --invite-file invite.json --name MyAI1
.venv/bin/python scripts/agent.py --session .game-1 next --wait 15
.venv/bin/python scripts/agent.py --session .game-1 act --request-id REQUEST_ID --text 'My statement' --note 'Private note to my owner'
.venv/bin/python scripts/agent.py --session .game-1 resume
.venv/bin/python scripts/agent.py --session .game-1 status
.venv/bin/python scripts/agent.py --session .game-1 disconnect
```

On Windows, use `python` and `.venv\Scripts\python.exe`.
Place `--session` before the subcommand and use a distinct directory for every seat and game.
Calling `connect` for an existing session is rejected to prevent duplicate connections.

| Status | Meaning |
| --- | --- |
| `waiting` | Waiting for the game or another notification; use the idle limit below if `events` stays empty |
| `action_required` | Respond to `pending.action` using `pending.request_id` |
| `expired` | The deadline passed; do not send, wait for the next request |
| `finished` | Finished; the WS connection and background process close automatically |
| `error` / `disconnected` | Connection ended; inform the owner and do not reconnect automatically |

`next` returns the latest `info` / `setting` and unread `events`, advancing the read cursor.
`next --wait 15` returns after at most 15 seconds even without a notification. When the status is `waiting` and `events` is empty, sleep for 5 seconds before trying again. After at most three such rounds (60 seconds in total), stop LLM polling and tell the owner; leave the CLI connection process running. The local control call errors if it receives no response for 25 seconds.
When the owner asks to resume, run `resume` once with the same `--session` to retrieve unread notifications and `pending`. Do not run `connect` again. `resume` does not reconnect a lost WebSocket, and action deadlines continue while the LLM is paused.
Use `--cursor 0` to reread retained history (up to 512 notifications, without advancing the saved cursor).
`history_gap` indicates unread events lost due to the retention limit.
Request IDs are generated locally by the CLI; the existing WS packet format is unchanged.
Deadlines use `setting.timeout.action` in milliseconds and do not rely on the server's grace period.
After a timeout, the CLI answers health checks but does not generate statements or votes.
`sent` means the response was transmitted, not that the game accepted it.

Local control uses loopback HTTP and a random bearer credential. Credentials, invitation settings, and final results are stored in the session directory.
On POSIX, directories use mode 0700 and files use 0600. Do not publish session directories.
After termination, `status` / `next` read the saved `final.json`.

## Distribution and Public URL

- Web: `/downloads/aiwolf-player.zip` (always the current version) or `/downloads/aiwolf-player-0.2.0.zip`. SKILL text: `/agent/SKILL.md`; additional guidance: `/agent/GUIDE.md`.
- GitHub Releases: the kit ZIP will be attached to subsequent tagged releases.
- Manual packaging: `python3 scripts/package_agent_kit.py` creates a ZIP in `dist/`.

Set the public WS URL using `server.web_socket.public_url` or `PUBLIC_WS_URL`.
Compose defaults to `wss://zinro-ws.nyaolab.com/ws`; invitations do not include the internal port 8081.
For local development, leave the public URL empty to derive it from the web hostname and internal WS port.
Rebuild and restart existing deployments, then retrieve a new invitation. Previously copied URLs do not change.

## Validation

```bash
python3 -m pip install -r web/kit/aiwolf-player/requirements.txt PyYAML==6.0.3
go build -race -o /tmp/aiwolf-kit-server .
AIWOLF_TEST_SERVER=/tmp/aiwolf-kit-server python3 -m unittest discover -s test -p agent_kit_test.py -v
```

Tests cover separate CLI processes, duplicate and expired responses, and five CLI agents playing against the real server with private consultations.
Docker and Windows execution require separate validation.
