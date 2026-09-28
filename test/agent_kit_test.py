"""CLIを別プロセスで実行し、WSの維持と期限・二重送信の境界を検証する。"""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import socket
import http.cookiejar
import urllib.request

from websockets.sync.server import serve

ROOT = Path(__file__).resolve().parents[1]
CLI = ROOT / "web/kit/aiwolf-player/scripts/agent.py"
spec = importlib.util.spec_from_file_location("agent_bridge", CLI)
agent = importlib.util.module_from_spec(spec)
spec.loader.exec_module(agent)


class FakeWS:
    def __init__(self): self.sent = []
    def send(self, value): self.sent.append(value)


class BridgeTests(unittest.TestCase):
    def setUp(self):
        self.bridge = agent.Bridge("AI1")
        self.bridge.ws = FakeWS()
        self.bridge.receive({"request": "INITIALIZE", "setting": {"timeout": {"action": 60000}}, "info": {"agent": "Player1", "status_map": {"Player1": "ALIVE", "Player2": "DEAD"}}})

    def test_duplicate_expired_and_name_probe(self):
        b = self.bridge
        b.receive({"request": "TALK"})
        rid = b.pending["request_id"]
        b.act(rid, "発言", "メモ")
        self.assertEqual(json.loads(b.ws.sent[-1])["note"], "メモ")
        with self.assertRaises(ValueError): b.act(rid, "二重", "")
        b.receive({"request": "VOTE"})
        rid = b.pending["request_id"]
        with self.assertRaises(ValueError): b.act(rid, "Player2", "")
        b.deadline = time.monotonic() - 1
        with self.assertRaises(ValueError): b.act(rid, "Player1", "")
        b.receive({"request": "NAME"})
        self.assertEqual(b.ws.sent[-1], "AI1")
        with self.assertRaises(ValueError): b.act(rid, "Player1", "")

    def test_freeform_and_malformed_invites_rejected(self):
        for mode in ("freeform", None):
            with self.assertRaises(ValueError): agent.validate_invite({"mode": mode, "ws_url": "ws://localhost/ws"})
        with self.assertRaises(ValueError):
            self.bridge.receive({"request": "INITIALIZE", "setting": {"talk": {"duration": 1000}}})

    def test_unread_history_is_preserved(self):
        b = self.bridge
        b.receive({"request": "DAILY_INITIALIZE", "info": {"owner_messages": ["助言"]}})
        first = b.next(0, 0)
        self.assertEqual(first["events"][-1]["owner_messages"], ["助言"])
        self.assertEqual(b.next(first["cursor"], 0)["events"], [])


class CLIProcessTests(unittest.TestCase):
    def test_cli_lifecycle(self):
        finished = threading.Event()
        proceed = threading.Event()
        received = []
        def handler(ws):
            ws.send(json.dumps({"request": "NAME"}))
            received.append(ws.recv(timeout=5))
            if not proceed.wait(10): return
            ws.send(json.dumps({"request": "INITIALIZE", "setting": {"timeout": {"action": 60000}}, "info": {"key_phrase": "private-test", "agent": "P1", "role_map": {"P1": "VILLAGER"}}}))
            ws.send(json.dumps({"request": "TALK", "info": {"key_phrase": "private-test", "owner_messages": ["様子を見て"]}}))
            received.append(json.loads(ws.recv(timeout=10)))
            ws.send(json.dumps({"request": "FINISH", "info": {"role_map": {"P1": "VILLAGER", "P2": "WEREWOLF"}}}))
            finished.set()

        with tempfile.TemporaryDirectory() as tmp, serve(handler, "127.0.0.1", 0) as server:
            threading.Thread(target=server.serve_forever, daemon=True).start()
            session = Path(tmp) / "session"
            invite = Path(tmp) / "invite.json"
            invite.write_text(json.dumps({"mode": "turn", "ws_url": "ws://127.0.0.1:%d/ws?room_id=test&seat_token=secret" % server.socket.getsockname()[1]}))
            def cli(*args, ok=True):
                p = subprocess.run([sys.executable, str(CLI), "--session", str(session), *args], capture_output=True, text=True, timeout=30)
                self.assertEqual(p.returncode, 0 if ok else 1, p.stdout + p.stderr)
                return json.loads(p.stdout)
            try:
                self.assertEqual(cli("connect", "--invite-file", str(invite), "--name", "AI1")["status"], "waiting")
                self.assertEqual(received, ["AI1"])
                self.assertEqual(cli("status")["status"], "waiting")
                self.assertEqual(cli("resume")["status"], "waiting")
                self.assertEqual(cli("resume")["events"], [])
                idle = cli("next", "--wait", "0.05")
                self.assertEqual((idle["status"], idle["events"]), ("waiting", []))
                self.assertEqual(cli("resume")["events"], [])
                cli("connect", "--invite-file", str(invite), "--name", "AI1", ok=False)
                proceed.set()
                end = time.monotonic() + 10
                while time.monotonic() < end:
                    out = cli("next", "--wait", "1")
                    if out["status"] == "action_required": break
                self.assertEqual(out["info"]["key_phrase"], "private-test")
                rid = out["pending"]["request_id"]
                self.assertEqual(cli("resume")["pending"]["request_id"], rid)
                self.assertEqual(cli("act", "--request-id", rid, "--text", "こんにちは", "--note", "確認中")["status"], "sent")
                self.assertTrue(finished.wait(5))
                self.assertEqual(received[-1], {"response": "こんにちは", "note": "確認中"})
                cli("act", "--request-id", rid, "--text", "再送", ok=False)
                out = cli("next", "--wait", "1")
                self.assertEqual(out["status"], "finished")
                end = time.monotonic() + 5
                while (session / "control.json").exists() and time.monotonic() < end: time.sleep(.05)
                self.assertFalse((session / "control.json").exists())
                self.assertEqual(cli("status")["status"], "finished")
                self.assertEqual(cli("resume")["status"], "finished")
                self.assertNotIn("secret", json.dumps(out))
            finally:
                if (session / "control.json").exists(): cli("disconnect")


@unittest.skipUnless(os.environ.get("AIWOLF_TEST_SERVER"), "実サーバのバイナリ指定時に実行")
class RealServerTests(unittest.TestCase):
    def test_five_cli_agents_complete_game(self):
        import yaml
        def port():
            with socket.socket() as s:
                s.bind(("127.0.0.1", 0))
                return s.getsockname()[1]
        web_port, ws_port = port(), port()
        while ws_port == web_port: ws_port = port()
        cfg = yaml.safe_load((ROOT / "config/default_5.yml").read_text())
        cfg["server"]["web"]["port"] = web_port
        cfg["server"]["web_socket"]["port"] = ws_port
        cfg["custom_profile"]["enable"] = False
        cfg["game"]["max_day"] = 5
        for key in ("json_logger", "game_logger", "realtime_broadcaster", "tts_broadcaster"):
            cfg[key]["enable"] = False
        clients = [urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())) for _ in range(5)]
        def api(i, path, body=None):
            req = urllib.request.Request("http://127.0.0.1:%d/api/v1/" % web_port + path,
                None if body is None else json.dumps(body).encode(), {"Content-Type": "application/json"})
            with clients[i].open(req, timeout=5) as res: return json.load(res)
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            config = work / "config.yml"
            config.write_text(yaml.safe_dump(cfg, allow_unicode=True))
            env = {k: v for k, v in os.environ.items() if k not in ("HOST", "PORT", "WEB_HOST", "WEB_PORT", "PUBLIC_WS_URL")}
            server_log = open(work / "server.log", "w+")
            server = subprocess.Popen([os.environ["AIWOLF_TEST_SERVER"], "-c", str(config)], cwd=work, env=env, stdout=server_log, stderr=server_log)
            def cli(i, *args):
                p = subprocess.run([sys.executable, str(CLI), "--session", str(work / ("session%d" % i)), *args], capture_output=True, text=True, timeout=30)
                self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
                return json.loads(p.stdout)
            try:
                end = time.monotonic() + 10
                while True:
                    try:
                        api(0, "healthz")
                        break
                    except OSError:
                        if server.poll() is not None:
                            server_log.seek(0)
                            self.fail(server_log.read())
                        if time.monotonic() > end: raise
                        time.sleep(.05)
                room = api(0, "rooms", {"user_name": "所有者0", "agent_count": 5, "mode": "participate"})
                path = "rooms/" + room["room_id"]
                for i in range(5):
                    if i: api(i, path + "/join", {"name": "所有者%d" % i, "mode": "participate"})
                    invite = work / ("invite%d.json" % i)
                    invite.write_text(json.dumps(api(i, path + "/invite")))
                    self.assertEqual(cli(i, "connect", "--invite-file", str(invite), "--name", "CLI%d" % i)["status"], "waiting")
                end = time.monotonic() + 5
                while api(0, path)["connected"] != 5 and time.monotonic() < end: time.sleep(.05)
                api(0, path + "/start", {})
                claimed, done, actions = set(), set(), set()
                wolf = None
                advice_seen = False
                end = time.monotonic() + 60
                while len(done) < 5 and time.monotonic() < end:
                    for i in range(5):
                        if i in done: continue
                        out = cli(i, "next", "--wait", "0")
                        self.assertNotIn(out["status"], ("error", "disconnected", "expired"))
                        info = out["info"]
                        if info.get("key_phrase") and i not in claimed:
                            api(i, path + "/claim", {"phrase": info["key_phrase"]})
                            api(i, path + "/consultations", {"text": "慎重に判断して"})
                            claimed.add(i)
                        if info.get("role_map", {}).get(info.get("agent")) == "WEREWOLF": wolf = info["agent"]
                        advice_seen |= any(e.get("owner_messages") for e in out["events"])
                        if out["status"] == "finished":
                            done.add(i)
                        elif out["status"] == "action_required":
                            pending = out["pending"]
                            actions.add(pending["action"])
                            text = "Over"
                            if pending["action"] not in ("TALK", "WHISPER"):
                                targets = [name for name, status in info["status_map"].items() if status == "ALIVE" and name != info["agent"]]
                                text = wolf if wolf in targets else targets[0]
                            cli(i, "act", "--request-id", pending["request_id"], "--text", text, "--note", "CLIテスト")
                self.assertEqual(len(done), 5)
                self.assertEqual(len(claimed), 5)
                self.assertTrue({"TALK", "DIVINE", "VOTE"}.issubset(actions), actions)
                self.assertTrue(advice_seen)
                self.assertEqual(api(0, path)["status"], "finished")
                history = api(0, path + "/consultations")["consultations"]
                self.assertTrue(any(e["type"] == "owner_note" for e in history))
            finally:
                for i in range(5):
                    if (work / ("session%d" % i) / "control.json").exists(): cli(i, "disconnect")
                server.kill()
                server.wait(timeout=5)
                server_log.close()


if __name__ == "__main__": unittest.main()
