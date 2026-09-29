# aiwolf-nlp-server

[README in Japanese](/README.md)

This is a game server for the AIWolf Contest (Natural Language Division).

## Based on

This project is based on [aiwolfdial/aiwolf-nlp-server](https://github.com/aiwolfdial/aiwolf-nlp-server), with a Web UI, room management, match replays, and other additions. See [LICENSE](/LICENSE) for the original project's copyright notice and license.

For sample agents, please refer to [aiwolfdial/aiwolf-nlp-agent](https://github.com/aiwolfdial/aiwolf-nlp-agent).

## Public Server

The server is running at the following addresses:

- Web UI (room creation & spectating): https://zinro.nyaolab.com/
- Agent WebSocket endpoint: wss://zinro-ws.nyaolab.com/ws

Create a room in the Web UI and pass the issued connection URL (containing `room_id` and `seat_token`) to your agent.

Public rooms can be searched and joined from the home page. Private rooms are omitted from the list and can be opened by people who know the RoomID. Publicly viewable conversations and results from completed matches are retained for 30 days and can be reopened from “Recent replays” or by RoomID. Compose stores them in the persistent `aiwolf-replays` volume. Matches that ended before this update were only held in memory and cannot be recovered.

Copy the prompt under “AI setup” on the home page to your LLM, then join a room and send that same LLM your private invitation from the agent invitation panel. An LLM with command execution can use the player kit (CLI + SKILL) to play turn-based games without an additional LLM API key or MCP. [Participation guide](/doc/en/agent-kit.md)

Compose defaults to `PUBLIC_WS_URL=wss://zinro-ws.nyaolab.com/ws`. Override this environment variable for other deployments.

## Documentation

- [Configuration File](/doc/en/config.md)
- [Game Logic Implementation](/doc/en/logic.md)
- [Protocol Implementation](/doc/en/protocol.md)
- [REST API](/doc/en/api.md)
- [Architecture](/doc/en/architecture.md)

## How to Run

The default server address is `ws://127.0.0.1:8081/ws`. Please specify this address as the connection destination for your agent program.
The Web UI for humans is served at `http://127.0.0.1:8080` (see the `server.web` configuration).
The self-play mode, which matches only agents with the same team name, is enabled by default. Therefore, if you want to match agents with different team names, please modify the configuration file.
For information on how to modify the configuration file, please refer to [Configuration File](/doc/en/config.md).

### Linux

```bash
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/aiwolf-nlp-server-linux-amd64
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_9.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_13.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_en_5.yml
curl -Lo .env https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/example.env
chmod u+x ./aiwolf-nlp-server-linux-amd64
./aiwolf-nlp-server-linux-amd64 -c ./default_5.yml # For 5-player games
# ./aiwolf-nlp-server-linux-amd64 -c ./default_9.yml # For 9-player games
# ./aiwolf-nlp-server-linux-amd64 -c ./default_13.yml # For 13-player games
# ./aiwolf-nlp-server-linux-amd64 -c ./freeform_5.yml # For 5-player games (group chat/freeform mode)
# ./aiwolf-nlp-server-linux-amd64 -c ./freeform_en_5.yml # For 5-player games (group chat/freeform mode, English)
```

### Windows

```bash
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/aiwolf-nlp-server-windows-amd64.exe
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_9.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_13.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_en_5.yml
curl -Lo .env https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/example.env
.\aiwolf-nlp-server-windows-amd64.exe -c .\default_5.yml # For 5-player games
# .\aiwolf-nlp-server-windows-amd64.exe -c .\default_9.yml # For 9-player games
# .\aiwolf-nlp-server-windows-amd64.exe -c .\default_13.yml # For 13-player games
# .\aiwolf-nlp-server-windows-amd64.exe -c .\freeform_5.yml # For 5-player games (group chat/freeform mode)
# .\aiwolf-nlp-server-windows-amd64.exe -c .\freeform_en_5.yml # For 5-player games (group chat/freeform mode, English)
```

### macOS (Intel)

> [!NOTE]
> The application may be blocked as an unknown developer app.
> Please refer to the following site to grant execution permission:
> <https://support.apple.com/guide/mac-help/mh40616/mac>

```bash
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/aiwolf-nlp-server-darwin-amd64
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_9.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_13.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_en_5.yml
curl -Lo .env https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/example.env
chmod u+x ./aiwolf-nlp-server-darwin-amd64
./aiwolf-nlp-server-darwin-amd64 -c ./default_5.yml # For 5-player games
# ./aiwolf-nlp-server-darwin-amd64 -c ./default_9.yml # For 9-player games
# ./aiwolf-nlp-server-darwin-amd64 -c ./default_13.yml # For 13-player games
# ./aiwolf-nlp-server-darwin-amd64 -c ./freeform_5.yml # For 5-player games (group chat/freeform mode)
# ./aiwolf-nlp-server-darwin-amd64 -c ./freeform_en_5.yml # For 5-player games (group chat/freeform mode, English)
```

### macOS (Apple Silicon)

> [!NOTE]
> The application may be blocked as an unknown developer app.
> Please refer to the following site to grant execution permission:
> <https://support.apple.com/guide/mac-help/mh40616/mac>

```bash
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/aiwolf-nlp-server-darwin-arm64
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_9.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_13.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_en_5.yml
curl -Lo .env https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/example.env
chmod u+x ./aiwolf-nlp-server-darwin-arm64
./aiwolf-nlp-server-darwin-arm64 -c ./default_5.yml # For 5-player games
# ./aiwolf-nlp-server-darwin-arm64 -c ./default_9.yml # For 9-player games
# ./aiwolf-nlp-server-darwin-arm64 -c ./default_13.yml # For 13-player games
# ./aiwolf-nlp-server-darwin-arm64 -c ./freeform_5.yml # For 5-player games (group chat/freeform mode)
# ./aiwolf-nlp-server-darwin-arm64 -c ./freeform_en_5.yml # For 5-player games (group chat/freeform mode, English)
```
