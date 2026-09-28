#!/usr/bin/env python3
"""人狼ノートのターン制接続CLI。LLMの判断と常駐する通信処理を分離する。"""

import argparse
import collections
import hmac
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

VERSION = "0.1.0"
ACTIONS = {"TALK", "WHISPER", "VOTE", "DIVINE", "GUARD", "ATTACK"}
TERMINAL = {"finished", "disconnected", "error"}


def save(path, value):
    # 接続トークンと役職を他ユーザーが読めないよう、置換前のファイルにも権限を設定する。
    temp = path.with_name(path.name + "." + secrets.token_hex(4))
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump(value, f, ensure_ascii=False)
    os.replace(temp, path)


def read(path):
    return json.loads(path.read_text(encoding="utf-8"))


class Bridge:
    def __init__(self, name):
        self.name = name
        self.cv = threading.Condition()
        self.ws = None
        self.status = "connecting"
        self.error = None
        self.info = {}
        self.setting = {}
        self.pending = None
        self.seq = 0
        self.events = collections.deque(maxlen=512)
        self.deadline = 0

    def emit(self, event):
        self.seq += 1
        self.events.append(dict(event, seq=self.seq))
        self.cv.notify_all()

    def receive(self, packet):
        with self.cv:
            req = packet["request"]
            # NAMEは接続確認にも使われる。期限切れの応答を次の要求へ流さない。
            self.pending = None
            if req == "NAME":
                self.ws.send(self.name)
                self.status = "waiting"
                self.emit({"type": "name_check" if self.setting else "connected",
                           "message": "生存確認に応答しました" if self.setting else "接続済みです。ホストの開始を待ちます。"})
                return
            if packet.get("setting"):
                self.setting = packet["setting"]
            if any(self.setting.get(k, {}).get("duration") is not None for k in ("talk", "whisper")) or "PHASE_" in req:
                raise ValueError("初版の参加キットはターン制専用です")
            if "info" in packet:
                self.info = packet["info"]
            event = {"type": req, "day": self.info.get("day")}
            for key in ("talk_history", "whisper_history"):
                if packet.get(key):
                    event[key] = packet[key]
            if self.info.get("owner_messages"):
                event["owner_messages"] = self.info["owner_messages"]
            self.emit(event)
            if req == "FINISH":
                self.status = "finished"
            elif req in ACTIONS:
                timeout = self.setting.get("timeout", {}).get("action")
                if not isinstance(timeout, (int, float)) or timeout <= 0:
                    raise ValueError("サーバの応答期限を取得できませんでした")
                self.deadline = time.monotonic() + timeout / 1000
                self.pending = {"request_id": secrets.token_hex(12), "action": req,
                                "deadline_at": time.time() + timeout / 1000}
                self.status = "action_required"
            else:
                self.status = "waiting"

    def snapshot(self, cursor=0):
        # サーバの猶予時間には頼らず、公開されたaction期限を越えた応答を拒否する。
        expired = self.pending is not None and time.monotonic() >= self.deadline
        pending = dict(self.pending) if self.pending else None
        if pending:
            pending["remaining_seconds"] = max(0, round(self.deadline - time.monotonic(), 2))
        return {"status": "expired" if expired else self.status, "error": self.error,
                "pending": pending, "info": self.info, "setting": self.setting,
                "events": [e for e in self.events if e["seq"] > cursor], "cursor": self.seq,
                "history_gap": bool(self.events and cursor < self.events[0]["seq"] - 1)}

    def next(self, cursor, wait):
        with self.cv:
            self.cv.wait_for(lambda: self.seq > cursor or (self.pending is not None and time.monotonic() < self.deadline) or self.status in TERMINAL, timeout=wait)
            return self.snapshot(cursor)

    def act(self, request_id, text, note):
        with self.cv:
            if not self.pending or self.pending["request_id"] != request_id:
                raise ValueError("要求IDが無効か、既に送信済みです")
            if time.monotonic() >= self.deadline:
                raise ValueError("応答期限が切れました。nextで次の要求を取得してください")
            if not isinstance(text, str) or not text.strip() or len(text) > 16000:
                raise ValueError("応答本文は1〜16000文字で指定してください")
            if not isinstance(note, str) or len(note) > 1000:
                raise ValueError("個別メモは1000文字以内で指定してください")
            req = self.pending["action"]
            if req not in {"TALK", "WHISPER"}:
                targets = self.info.get("status_map", {})
                no_attack = req == "ATTACK" and self.setting.get("attack_vote", {}).get("allow_no_target") and text == "None"
                if not no_attack and targets.get(text) != "ALIVE":
                    raise ValueError("対象はstatus_mapの生存者名を正確に指定してください")
            # 送信失敗時も同じ要求を自動再送しない。届いたか不明な二重投票を防ぐ。
            self.pending = None
            self.status = "waiting"
            try:
                self.ws.send(json.dumps({"response": text, "note": note}, ensure_ascii=False))
            except Exception:
                self.status = "error"
                self.error = "応答の送信に失敗しました。自動再送はしません"
                self.cv.notify_all()
                raise ValueError(self.error) from None
            return {"status": "sent", "request_id": request_id}


def validate_invite(invite):
    if invite.get("mode") != "turn":
        raise ValueError("ターン制の招待設定が必要です。Webから最新のinvite.jsonを取得してください")
    u = urllib.parse.urlsplit(invite["ws_url"])
    if u.scheme not in ("ws", "wss") or not u.hostname or u.username or u.fragment:
        raise ValueError("接続URLが不正です")
    q = urllib.parse.parse_qs(u.query)
    if not q.get("room_id") or not q.get("seat_token"):
        raise ValueError("招待設定にRoomIDまたは席トークンがありません")


def daemon(session, name):
    from websockets.sync.client import connect

    invite = read(session / "invite.json")
    bridge = Bridge(name)
    token = secrets.token_urlsafe(32)
    stopping = threading.Event()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            # ブラウザから操作できないよう、ランダムなBearer値とOrigin不在の両方を要求する。
            if self.headers.get("Origin") or not hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + token):
                self.send_error(403)
                return
            try:
                size = int(self.headers.get("Content-Length", 0))
                if not 0 < size <= 131072:
                    raise ValueError("リクエストサイズが不正です")
                body = json.loads(self.rfile.read(size))
                if self.path == "/next":
                    out = bridge.next(int(body.get("cursor", 0)), min(20, max(0, float(body.get("wait", 0)))))
                elif self.path == "/status":
                    with bridge.cv:
                        out = bridge.snapshot(bridge.seq)
                elif self.path == "/act":
                    out = bridge.act(body.get("request_id"), body.get("text"), body.get("note", ""))
                elif self.path == "/disconnect":
                    stopping.set()
                    if bridge.ws:
                        bridge.ws.close()
                    out = {"status": "disconnected"}
                else:
                    raise ValueError("操作が不正です")
                code = 200
            except (ValueError, TypeError, KeyError) as exc:
                out, code = {"error": str(exc)}, 400
            data = json.dumps(out, ensure_ascii=False).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    save(session / "control.json", {"port": server.server_port, "token": token})
    try:
        with connect(invite["ws_url"], open_timeout=10, close_timeout=2, max_size=2**20) as ws:
            bridge.ws = ws
            while not stopping.is_set():
                try:
                    raw = ws.recv(timeout=0.25)
                except TimeoutError:
                    continue
                bridge.receive(json.loads(raw))
                if bridge.status == "finished":
                    break
    except ValueError as exc:
        with bridge.cv:
            bridge.error = str(exc)
            bridge.status = "error"
    except Exception:
        # ライブラリ例外には招待URLが含まれることがあるため、そのまま記録しない。
        with bridge.cv:
            bridge.error = "接続が終了しました。接続先・ルーム状態・通信環境を確認してください（自動再接続なし）"
            bridge.status = "error"
    finally:
        with bridge.cv:
            bridge.pending = None
            if stopping.is_set():
                bridge.status, bridge.error = "disconnected", None
            elif bridge.status not in TERMINAL:
                bridge.status = "disconnected"
            bridge.emit({"type": bridge.status})
            save(session / "final.json", bridge.snapshot())
        server.shutdown()
        server.server_close()
        (session / "control.json").unlink(missing_ok=True)


def control(session, command, body):
    final = session / "final.json"
    if final.exists():
        if command == "act":
            raise ValueError("接続は終了しています")
        data = read(final)
        data["events"] = [e for e in data["events"] if e["seq"] > body.get("cursor", 0)]
        return data
    cfg = read(session / "control.json")
    req = urllib.request.Request("http://127.0.0.1:" + str(cfg["port"]) + "/" + command,
                                 json.dumps(body).encode(),
                                 {"Authorization": "Bearer " + cfg["token"], "Content-Type": "application/json"})
    # ローカル制御はHTTP_PROXY等へ流さない。
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req, timeout=25) as res:
            return json.load(res)
    except urllib.error.HTTPError as exc:
        raise ValueError(json.load(exc).get("error", "CLI操作に失敗しました")) from None
    except urllib.error.URLError:
        if final.exists():
            return control(session, command, body)
        raise ValueError("接続プロセスに到達できません。statusを確認してください") from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", action="version", version=VERSION)
    parser.add_argument("--session", type=Path, default=Path(".aiwolf-session"), help="席ごとに異なる保存先を指定")
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("connect")
    p.add_argument("--invite-file", type=Path, required=True)
    p.add_argument("--name", required=True)
    p = sub.add_parser("next")
    p.add_argument("--wait", type=float, default=20)
    p.add_argument("--cursor", type=int, help="省略時は前回取得位置から差分を返す")
    p = sub.add_parser("act")
    p.add_argument("--request-id", required=True)
    p.add_argument("--text", required=True)
    p.add_argument("--note", default="")
    sub.add_parser("status")
    sub.add_parser("disconnect")
    p = sub.add_parser("_serve", help=argparse.SUPPRESS)
    p.add_argument("--name", required=True)
    args = parser.parse_args()
    session = args.session.resolve()
    try:
        if args.command == "_serve":
            daemon(session, args.name)
            return
        if args.command == "connect":
            try:
                import websockets.sync.client  # noqa: F401
            except ImportError:
                raise ValueError("先に python -m pip install -r requirements.txt を実行してください") from None
            if not args.name.strip() or len(args.name) > 64 or any(c in args.name for c in "\r\n"):
                raise ValueError("AI名は改行なしの1〜64文字にしてください")
            invite = read(args.invite_file)
            validate_invite(invite)
            # 再実行で二重接続しない。試合ごとに新しい保存先を使う。
            session.mkdir(mode=0o700, parents=True, exist_ok=False)
            save(session / "invite.json", invite)
            options = {"start_new_session": True} if os.name != "nt" else {"creationflags": subprocess.DETACHED_PROCESS | subprocess.CREATE_NEW_PROCESS_GROUP}
            proc = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "--session", str(session), "_serve", "--name", args.name],
                                    stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, **options)
            end = time.monotonic() + 12
            while time.monotonic() < end:
                if (session / "control.json").exists() or (session / "final.json").exists():
                    out = control(session, "status", {})
                    if out["status"] != "connecting":
                        break
                if proc.poll() is not None:
                    raise ValueError("接続プロセスを起動できませんでした")
                time.sleep(0.05)
            else:
                proc.terminate()
                proc.wait(timeout=5)
                raise ValueError("接続確認がタイムアウトしました")
        else:
            body = {}
            if args.command == "next":
                cursor_file = session / "cursor.json"
                cursor = args.cursor if args.cursor is not None else (read(cursor_file) if cursor_file.exists() else 0)
                body = {"cursor": cursor, "wait": args.wait}
            elif args.command == "act":
                body = {"request_id": args.request_id, "text": args.text, "note": args.note}
            out = control(session, args.command, body)
            if args.command == "next" and args.cursor is None:
                save(session / "cursor.json", out["cursor"])
        print(json.dumps(out, ensure_ascii=False))
        if out.get("status") == "error":
            sys.exit(1)
    except (ValueError, OSError, KeyError) as exc:
        message = str(exc) if isinstance(exc, ValueError) else "設定ファイル・セッション保存先を確認してください。既存の保存先ではstatusを使ってください"
        print(json.dumps({"status": "error", "error": message}, ensure_ascii=False))
        sys.exit(1)


if __name__ == "__main__":
    main()
