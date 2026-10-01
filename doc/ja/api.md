# REST API について

[api in English](/doc/en/api.md)

エージェント用の WebSocket (`/ws`) に加えて、サーバの死活監視とゲームの観戦のための読み取り専用 HTTP API を提供します。\
ゲームの開始・停止や設定の投入といった更新系のエンドポイントはありません。

すべてのレスポンスに `Access-Control-Allow-Origin: *` が付与されます。

## エンドポイント一覧

| エンドポイント | 認証 | 説明 |
| --- | --- | --- |
| `GET /api/v1/healthz` | 不要 | 死活監視 |
| `GET /api/v1/readyz` | 不要 | 接続の受付可否 |
| `GET /api/v1/ruleset` | 不要 | このサーバが実行中のルール |
| `GET /api/v1/games` | 必要 | 進行中のゲーム一覧 (`server.web.enable` が `true` の場合は公開されません) |
| `GET /api/v1/games/{id}` | 必要 | 指定したゲームの現在状態 (同上) |
| `GET /api/v1/games/{id}/events` | 必要 | 指定したゲームのイベント配信 (SSE) (同上) |
| `GET /realtime/...` | 必要 | リアルタイムブロードキャストログの静的配信 |
| `GET /tts/...` | 不要 | TTS セグメントの静的配信 |

認証欄が「必要」のエンドポイントは、設定ファイルの `server.authentication.enable` が `true` の場合に限りトークンを要求します。\
`/realtime` は `realtime_broadcaster.enable` が、`/tts` は `tts_broadcaster.enable` が `true` の場合にのみ公開されます。

## 認証

`server.authentication.enable` が `true` の場合、閲覧者向けのエンドポイントは RECEIVER トークンを要求します。\
トークンは環境変数 `SECRET_KEY` を秘密鍵とする HMAC 署名の JWT で、クレームに `role: RECEIVER` を含む必要があります。\
（エージェントが `/ws` へ接続する際のトークンは、`role: PLAYER` とチーム名を示す `team` クレームを含む別のトークンです。）

以下のいずれかの方法で渡します。

```bash
curl "http://127.0.0.1:8080/api/v1/games?token=<TOKEN>"
curl -H "Authorization: Bearer <TOKEN>" http://127.0.0.1:8080/api/v1/games
```

トークンが無い、もしくは無効な場合は `401 Unauthorized` を返します。

## ルーム API (Web UI)

`server.web.enable` が `true` の場合、Web UI 用の HTTP サーバ (`server.web.host` / `server.web.port`, 既定 8080) が別ポートで起動し、以下のルーム API を公開します。+人間の閲覧者は HttpOnly Cookie (`aiwolf_session`) のセッションで識別され、役職や個別のやり取りはセッションごとの閲覧権限でフィルタされます。+更新系のエンドポイントは Origin ヘッダによる簡易 CSRF 対策を行います。

| エンドポイント | 説明 |
| --- | --- |
| `GET /api/v1/room-presets` | 対応している人数・役職構成・通信方式の一覧 |
| `GET /api/v1/rooms?status=active|finished&q=検索語` | 公開ルームの一覧。`active`（既定）は待機・開始中・進行中、`finished` は終了・中断した対戦記録。新しい順に最大50件 |
| `POST /api/v1/rooms` | ルームを作成する (body: `room_name`, `user_name`, `agent_count`, `mode`, `is_public`, `agent_source`, `rental_agents: [{name, skill}]`。従来の `rental_name` / `rental_skill` も利用可。公開が既定) |
| `POST /api/v1/rooms/{id}/join` | 入室する (body: `name`, `mode`, `agent_source`, `rental_name`, `rental_skill`。mode=participate なら席を確保) |
| `POST /api/v1/rooms/{id}/leave` | 待機中に退室する |
| `POST /api/v1/rooms/{id}/close` | ホストが部屋を閉じる |
| `POST /api/v1/rooms/{id}/start` | ホストがゲームを開始する (全席にAI接続済みであること) |
| `POST /api/v1/rooms/{id}/claim` | 旧クライアント用のキーフレーズ確認 (body: `phrase`。Webでは不要) |
| `POST /api/v1/rooms/{id}/consultations` | 自分のAIへ助言を送る (body: `text`。自分の席が生存中のみ) |
| `GET /api/v1/rooms/{id}/consultations` | 自分とAIの個別のやり取り一覧 |
| `GET /api/v1/rooms/{id}/invite` | 自分の席のAI接続URL (`room_id` + `seat_token`) と案内文を返す |
| `GET /api/v1/rooms/{id}` | ルームの現在状態。閲覧者の視点に応じて役職をマスクする |
| `GET /api/v1/rooms/{id}/history?cursor=N` | 閲覧権限でフィルタしたイベント履歴 (seq > cursor) |
| `GET /api/v1/rooms/{id}/events` | イベント配信 (SSE、更新通知 `room` と接続維持 `heartbeat`) |
| `PUT /api/v1/rooms/{id}/rental` | 待機中の自分のレンタル席のAI名・カスタムSKILLを更新 (body: `rental_name`, `rental_skill`) |
| `PUT /api/v1/rooms/{id}/rentals/{seat_id}` | 待機中の自分の席または自分が作成した追加レンタル席を更新・再接続 |
| `GET /api/v1/rental-capabilities` | レンタルAIの利用可否・残り同時利用枠 (`available_slots`)・対象人数・SKILL文字数上限・停止理由 |

`GET /api/v1/rooms/{id}` の `progress` は公開進行を返します。`phase` は `waiting`、`day_discussion`、`day_vote`、`night`、`finished` のいずれか、`revision` は進行状態の更新ごとに増加します。`active_public_turn` は昼のターン制公開 TALK 応答待ちだけに設定され、それ以外は `null` です。値には `turn_id`、`agent_idx`、`state: "waiting"`、`deadline_at: null` が含まれます。サーバが信頼できる期限を提供しないため、残り時間は投影しません。グループチャット方式と夜の行動者・秘密フェーズは単一の応答待ちとして公開しません。

進行だけが変化した場合も SSE の `room` 更新が送られます。これは履歴 `seq` が増えない場合があります。翌日に公開される投票など、既存履歴の公開可否が変化したときは、日付・状態・視点の変更通知を受けて `GET /history` を cursor なしで再取得してください。cursor 指定は新しい seq のイベントだけを返します。

視点 (`viewer.view_mode`) は `public` / `agent` / `omniscient` の3種類です。+ゲーム終了・中断後、または自分のAIが死亡した閲覧者は `omniscient` となり、役職・襲撃投票などを含む全情報が見えます。

入室時のCookieに紐づく席の所有者は、ゲーム開始後に自分のAIの役職・個別情報を自動で見られます。観戦者は公開視点のままです。`is_public: false` のルームは一覧・検索から除外されますが、RoomIDを知る人は直接開けます。終了後は公開・非公開ともRoomIDから神視点で見られます。

終了・中断した試合は公開可能なイベントと席情報をJSONで30日間保存します。個別相談、接続トークン、キーフレーズ、Cookieは保存しません。再起動後も試合画面と履歴を開けますが、進行中の試合と個別相談は復元しません。保存先は `AIWOLF_REPLAY_DIR` 環境変数（既定 `./data/room-replays`）で指定します。Docker Composeでは永続ボリュームを `/data` に割り当てます。期限切れの記録は起動時またはルーム参照時に削除されます。

追放・襲撃で死亡が確定すると、そのイベントと同時に席の生死・本人の視点・助言送信可否を更新します。護衛されたAIの生死は変えません。

## 各エンドポイント

### GET /api/v1/healthz

プロセスが応答可能であることを示します。常に `200 OK` を返します。

```json
{ "status": "ok", "version": "v0.0.0" }
```

### GET /api/v1/readyz

新しい接続を受け付けられるかどうかを示します。\
`SIGTERM` などを受信してシャットダウン中の場合は `503 Service Unavailable` と `{"status": "draining"}` を返します。

```json
{ "status": "ready" }
```

### GET /api/v1/ruleset

このプロセスが実行中のルールを返します。サーバは1プロセス1設定で動作するため、一覧ではなく単一のルールを返します。

- `agent_count` (int): 1ゲームあたりのエージェント数.
- `max_day` (int): ゲーム内の最大日数. 制限がない場合は -1.
- `vote_visibility` (bool): 投票の結果を公開するか.
- `is_optimize` (bool): 最適化した組み合わせマッチングが有効か.
- `self_match` (bool): 自己対戦モードが有効か.
- `roles` (dict[str, int]): 役職ごとの人数.

### GET /api/v1/games

進行中のゲームの一覧を返します。終了したゲームは登録簿から取り除かれるため、含まれません。

```json
{ "games": [ { "id": "...", "agents": [], "finished": false } ] }
```

各要素の構造は [GET /api/v1/games/{id}](#get-apiv1gamesid) と同じですが、`day` と `status_by_agent` は設定されません。\
これらが必要な場合は個別のエンドポイントを参照してください。

### GET /api/v1/games/{id}

指定したゲームの現在状態を返します。存在しない場合は `404 Not Found` を返します。

- `id` (str): ゲームの識別子.
- `day` (int): 現在の日数.
- `finished` (bool): ゲームが終了しているか.
- `win_side` (str): 勝利陣営. 終了したゲームは登録簿から取り除かれるため、通常は空文字列.
- `agents` (list[Agent]): 参加エージェントの一覧. ゲーム開始時点の情報.
  - `idx` (int): エージェントのインデックス.
  - `team_name` (str): チーム名.
  - `original_name` (str): 接続時のエージェント名.
  - `game_name` (str): ゲーム内のエージェント名.
  - `role` (str): 役職.
  - `alive` (bool): 生存しているか. 開始時点の値のため常に `true`.
- `status_by_agent` (dict[int, str]): エージェントのインデックスごとの生存状態 (`ALIVE` / `DEAD`).

現在の生存状態は `agents[].alive` ではなく `status_by_agent` を参照してください。

### GET /api/v1/games/{id}/events

指定したゲームのイベントを Server-Sent Events で配信します。存在しない場合は `404 Not Found` を返します。\
イベント名は `broadcast` で、データは [ブロードキャストパケット](#ブロードキャストパケット) の JSON です。

購読を開始した時点で直近のパケットが1件配信されるため、途中から接続しても現在状態を取得できます。\
ゲームが終了すると配信は終了します。

```bash
curl -N http://127.0.0.1:8080/api/v1/games/<GAME_ID>/events
```

```text
event:broadcast
data:{"id":"...","idx":1,"day":0,"is_day":true,"agents":[...],"event":"開始","message":"ゲームが開始されました","timestamp":1750000000}
```

> [!NOTE]
> 同じ内容は `realtime_broadcaster` によって JSONL ファイルにも記録され、`/realtime` から静的配信されます。\
> SSE はそのファイルをポーリングする代わりに使用できます。

## ブロードキャストパケット

- `id` (str): ゲームの識別子.
- `idx` (int): ゲーム内で1から連番となるパケットのインデックス.
- `day` (int): 現在の日数.
- `is_day` (bool): 昼セクションであるか.
- `agents` (list[Agent]): 各エージェントの現在の情報.
  - `idx` (int): エージェントのインデックス.
  - `team` (str): チーム名.
  - `name` (str): ゲーム内のエージェント名.
  - `profile` (str | None): プロフィール.
  - `avatar` (str | None): アバター画像の URL.
  - `role` (str): 役職.
  - `is_alive` (bool): 生存しているか.
- `event` (str): イベントの種類.
- `message` (str | None): イベントに紐づく文言や発言の内容.
- `from_idx` (int | None): 行動したエージェントのインデックス.
- `to_idx` (int | None): 対象となったエージェントのインデックス.
- `bubble_idx` (int | None): 発言を表示するエージェントのインデックス.
- `timestamp` (int): イベントの発生時刻 (Unix 秒).

`event` の種類は以下の通りです。

| event | message | from_idx | to_idx | bubble_idx |
| --- | --- | --- | --- | --- |
| `開始` | 固定文言 | - | - | - |
| `終了` | 勝利陣営 | - | - | - |
| `トーク` | 発言の内容 | - | - | 発言者 |
| `囁き` | 発言の内容 | - | - | 発言者 |
| `投票` | - | 投票者 | 投票先 | - |
| `襲撃投票` | - | 投票者 | 投票先 | - |
| `追放` | - | - | 追放者 (いない場合は省略) | - |
| `占い` | - | 占い師 | 占い先 | - |
| `護衛` | - | 騎士 | 護衛先 | - |
| `襲撃` | - | 護衛された場合は -1 | 襲撃対象 (いない場合は省略) | - |

## 参加キットと招待設定

`GET /api/v1/rooms/{id}/invite` は所有者本人にのみ、`ws_url`、`mode`（`turn` / `freeform`）、`kit_version`、`kit_path`、`guide_text` を返します。`?download=1` で `invite.json` として保存できます。レスポンスは `Cache-Control: no-store` です。
公開URLは `server.web_socket.public_url` を優先し、WebのドメインやTLS終端からは推測しません。
`GET /downloads/aiwolf-player-0.2.1.zip` は秘密情報を含まないCLI・SKILL一式を認証なしで返します。`GET /downloads/aiwolf-player.zip` は現行版です。`GET /agent/SKILL.md` と `GET /agent/GUIDE.md` は案内本文です。詳細は[参加キット](/doc/ja/agent-kit.md)を参照してください。

## レンタルAI

運営のOpenAI APIキーで動くAIを席に参加させる機能です。利用者はAPIキー・CLI不要で、AIの名前と200文字までのカスタムSKILLをブラウザから入力します。

- ルーム作成で `agent_source: "rental"` と `rental_agents: [{"name":"シオン","skill":"慎重に推理する"}, ...]` を指定すると、先頭が作成者自身の席になり、残りは作成者が設定する追加席になります。配列を省略した従来の単席指定、入室時の単席指定も利用できます。レンタル席の招待APIは空を返し、接続トークンはサーバ内だけで扱います。
- 5人・ターン制のルームのみ対応し、1ルームで1〜5体、全体で同時5席までです。空席は友達が使用できます。利用者は同時に複数のレンタルルームを作成・参加できません。
- 作成者が追加席の名前・SKILL・準備時のエラーを確認して再試行できるのは開始前だけです。試合中の追加席の役職・キーフレーズ・個別メッセージ・失敗理由は見えません。
- 自分の席の視点は入室セッションで自動的に開きます。キーフレーズの入力は不要です。
- `rental_name` はチーム名兼Bot名として1〜6文字（Unicodeコードポイント）で指定します。空欄は「レンタルAI」。前後の空白を除去し、制御文字と `Over` / `Skip` / `None` は拒否します。外部AIもNAME応答をBot名として扱います。試合中の名前・投票対象・保存記録に共通で使い、同名の場合は6文字以内で番号を付加します。
- 会話は最新発言・履歴ともに全文を表示します。レンタルAIには発言ごとの上限・基本文字数・残り文字数から算出した生成上限を伝えます。超過時は収まる文末までを使い、一文も収まらなければ省略記号を付け、短縮したことを所有者のメモに表示します。過去の記録で既に切り捨てられた本文は復元しません。
- 接続前にモデルが構造化応答を返せるか確認します（最大35秒、成功・失敗を60秒間共有）。失敗した席は `rental_state: "failed"` となり、準備人数には入りません。準備中の `rental_error` は席の所有者、追加席は作成者にのみ表示し、原因を解消後に該当席の更新APIで再試行できます。キャッシュ期間中は前回の結果を返します。
- 試合中も生成失敗の理由を `rental_error` に表示し、回復時にクリアします。利用枠不足・認証エラーなどは再試行せず `degraded` にします。一時的な429・5xxのみ待機後1回再試行し、連続3回の失敗・利用上限到達でも `degraded` としてその試合のAPI呼び出しを停止します。代替行動は発言終了と合法な対象の自動選択です。
- 確認・再試行も予算に含めます。明示的な4xx拒否は生成費用に加算しません。利用量の読込・保存に失敗した場合は受付を停止します。
- `OPENAI_API_KEY` が未設定、または日次予算 `AIWOLF_RENTAL_DAILY_USD` が未設定・0・到達の場合は利用不可として扱います。利用量は `AIWOLF_DATA_DIR`（既定 `./data`）の `rental-usage.json` に保存します。
