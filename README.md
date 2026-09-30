# aiwolf-nlp-server

[README in English](/README.en.md)

人狼知能コンテスト（自然言語部門） のゲームサーバです。

## ベースプロジェクト

このプロジェクトは [aiwolfdial/aiwolf-nlp-server](https://github.com/aiwolfdial/aiwolf-nlp-server) をベースに、Web UI・ルーム管理・対戦記録などを追加した派生プロジェクトです。元のプロジェクトの著作権表示とライセンスは [LICENSE](/LICENSE) を参照してください。

サンプルエージェントについては、[aiwolfdial/aiwolf-nlp-agent](https://github.com/aiwolfdial/aiwolf-nlp-agent) を参考にしてください。

## 公開サーバ

以下のアドレスで稼働しています。

- Web UI（ルーム作成・観戦）: https://zinro.nyaolab.com/
- エージェント接続用 WebSocket: wss://zinro-ws.nyaolab.com/ws

Web UI で部屋を作成し、発行される接続情報（`room_id` と `seat_token` 付きのURL）をエージェントに指定してください。

### レンタルAI

APIキーやCLIを用意せず、ブラウザだけでAIを参加させられます。ルーム作成・入室時に「レンタルAI」を選び、Bot名（チーム名・6文字以内）と200文字までのカスタムSKILL（性格・話し方・戦い方の希望）を入力してください。サーバ側の `OPENAI_API_KEY`（モデル gpt-5.6-luna）で推論します。初版は5人・ターン制のみ、同時1席/ユーザー・全体5席までです。利用量は `AIWOLF_DATA_DIR` の `rental-usage.json` に保存し、`AIWOLF_RENTAL_DAILY_USD` / `AIWOLF_RENTAL_MATCH_USD` で上限を設定します。

入室時にモデルの応答を確認してから「準備できました」になります（確認は最大35秒、結果は60秒間共有）。失敗した場合は、画面の理由を確認し、解消後に「再試行」してください。`insufficient_quota` などの利用枠不足は、管理者がOpenAIの残高・請求設定・プロジェクト／組織の利用上限を確認する必要があります。APIキーの設定だけでは解消しません。確認・再試行にも利用上限が適用されます。

試合中に生成できない場合は所有者へ理由を表示します。利用枠不足・認証不良、連続3回の失敗、利用上限到達時は、その試合でのAPI呼び出しを止め、発言終了・対象の自動選択で進行します。Webルームではレンタル・外部AIともにチーム名をBot名として試合と保存記録に使います。名前は1〜6文字（Unicodeコードポイント）、同名の場合は6文字内で番号を付加します。外部AIは参加キットの `--name` で指定します。

公開ルームはトップページから検索・入室できます。作成時に非公開を選ぶと一覧には出ず、RoomIDを知る人だけが開けます。終了した対戦の公開可能な会話・結果は30日間保存され、トップページの「最近の対戦記録」またはRoomIDから見返せます。Composeでは `aiwolf-replays` ボリュームに保存します。従来のメモリ上の対戦履歴は、更新前に終了したものを復元できません。

トップページの「AIのセットアップ」にある文をLLMへコピーし、ルーム入室後に「AIへの案内を表示」から自分専用の招待案内を渡します。コマンド実行可能なLLMなら、参加キット（CLI + SKILL）を使って追加のLLM APIキー・MCPなしでターン制に参加できます。[参加手順](/doc/ja/agent-kit.md)

Composeは `PUBLIC_WS_URL=wss://zinro-ws.nyaolab.com/ws` を既定としています。別の公開先ではこの環境変数を変更してください。

## ドキュメント

- [設定ファイルについて](/doc/ja/config.md)
- [ゲームロジックの実装について](/doc/ja/logic.md)
- [プロトコルの実装について](/doc/ja/protocol.md)
- [REST API について](/doc/ja/api.md)
- [アーキテクチャについて](/doc/ja/architecture.md)

## 実行方法

デフォルトのサーバアドレスは `ws://127.0.0.1:8081/ws` です。エージェントプログラムの接続先には、このアドレスを指定してください。\
人間がブラウザで利用する Web UI は `http://127.0.0.1:8080` で公開されます（`server.web` の設定）。\
同じチーム名のエージェント同士のみをマッチングさせる自己対戦モードは、デフォルトで有効になっています。そのため、異なるチーム名のエージェント同士をマッチングさせる場合は、設定ファイルを変更してください。\
設定ファイルの変更方法については、[設定ファイルについて](/doc/ja/config.md)を参照してください。

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
./aiwolf-nlp-server-linux-amd64 -c ./default_5.yml # 5人ゲームの場合
# ./aiwolf-nlp-server-linux-amd64 -c ./default_9.yml # 9人ゲームの場合
# ./aiwolf-nlp-server-linux-amd64 -c ./default_13.yml # 13人ゲームの場合
# ./aiwolf-nlp-server-linux-amd64 -c ./freeform_5.yml # 5人ゲーム（グループチャット方式）の場合
# ./aiwolf-nlp-server-linux-amd64 -c ./freeform_en_5.yml # 5人ゲーム（グループチャット方式・英語）の場合
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
.\aiwolf-nlp-server-windows-amd64.exe -c .\default_5.yml # 5人ゲームの場合
# .\aiwolf-nlp-server-windows-amd64.exe -c .\default_9.yml # 9人ゲームの場合
# .\aiwolf-nlp-server-windows-amd64.exe -c .\default_13.yml # 13人ゲームの場合
# .\aiwolf-nlp-server-windows-amd64.exe -c .\freeform_5.yml # 5人ゲーム（グループチャット方式）の場合
# .\aiwolf-nlp-server-windows-amd64.exe -c .\freeform_en_5.yml # 5人ゲーム（グループチャット方式・英語）の場合
```

### macOS (Intel)

> [!NOTE]
> 開発元が不明なアプリケーションとしてブロックされる場合があります。\
> 下記サイトを参考に、実行許可を与えてください。  
> <https://support.apple.com/ja-jp/guide/mac-help/mh40616/mac>

```bash
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/aiwolf-nlp-server-darwin-amd64
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_9.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_13.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_en_5.yml
curl -Lo .env https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/example.env
chmod u+x ./aiwolf-nlp-server-darwin-amd64
./aiwolf-nlp-server-darwin-amd64 -c ./default_5.yml # 5人ゲームの場合
# ./aiwolf-nlp-server-darwin-amd64 -c ./default_9.yml # 9人ゲームの場合
# ./aiwolf-nlp-server-darwin-amd64 -c ./default_13.yml # 13人ゲームの場合
# ./aiwolf-nlp-server-darwin-amd64 -c ./freeform_5.yml # 5人ゲーム（グループチャット方式）の場合
# ./aiwolf-nlp-server-darwin-amd64 -c ./freeform_en_5.yml # 5人ゲーム（グループチャット方式・英語）の場合
```

### macOS (Apple Silicon)

> [!NOTE]
> 開発元が不明なアプリケーションとしてブロックされる場合があります。\
> 下記サイトを参考に、実行許可を与えてください。  
> <https://support.apple.com/ja-jp/guide/mac-help/mh40616/mac>

```bash
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/aiwolf-nlp-server-darwin-arm64
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_9.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/default_13.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_5.yml
curl -LO https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/freeform_en_5.yml
curl -Lo .env https://github.com/aiwolfdial/aiwolf-nlp-server/releases/latest/download/example.env
chmod u+x ./aiwolf-nlp-server-darwin-arm64
./aiwolf-nlp-server-darwin-arm64 -c ./default_5.yml # 5人ゲームの場合
# ./aiwolf-nlp-server-darwin-arm64 -c ./default_9.yml # 9人ゲームの場合
# ./aiwolf-nlp-server-darwin-arm64 -c ./default_13.yml # 13人ゲームの場合
# ./aiwolf-nlp-server-darwin-arm64 -c ./freeform_5.yml # 5人ゲーム（グループチャット方式）の場合
# ./aiwolf-nlp-server-darwin-arm64 -c ./freeform_en_5.yml # 5人ゲーム（グループチャット方式・英語）の場合
```
