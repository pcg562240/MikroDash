"""Local synthetic API fixtures for UI QA. Never connects to a real router.

Mihomo: http://127.0.0.1:19090 (token: fixture-only)
RouterOS binary API: 127.0.0.1:19091 (any test login, fixture data only)
"""
import http.server
import json
import math
import socketserver
import threading
import time

START = time.monotonic()
DEVICES = [("工作电脑", "02:00:00:00:00:01", "192.168.20.10", 1300000, 90000),
           ("手机", "02:00:00:00:00:02", "192.168.20.11", 350000, 60000),
           ("NAS", "02:00:00:00:00:03", "192.168.20.12", 60000, 260000),
           ("旁路由出口", "02:00:00:00:00:04", "192.168.20.250", 1710000, 410000)]


class Controller(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def do_GET(self):
        if self.headers.get("Authorization") != "Bearer fixture-only":
            self.send_error(401); return
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers()
        if self.path == "/connections":
            self.wfile.write(json.dumps({"connections": [{}] * 34}).encode()); return
        if self.path != "/traffic": return
        try:
            while True:
                t = time.monotonic() - START
                out = {"up": int(420000 + 190000 * math.sin(t/7)),
                       "down": int(2500000 + 1200000 * math.sin(t/11)),
                       "upTotal": 2500000000 + int(t*420000),
                       "downTotal": 14500000000 + int(t*2500000)}
                self.wfile.write((json.dumps(out) + "\n").encode()); self.wfile.flush(); time.sleep(1)
        except (ConnectionError, BrokenPipeError): pass


def word(w):
    b = w.encode()
    n = len(b)
    prefix = bytes([n]) if n < 128 else (n | 0x8000).to_bytes(2, "big")
    return prefix + b


class ROS(socketserver.StreamRequestHandler):
    def sentence(self):
        result = []
        while True:
            b = self.rfile.read(1)
            if not b: raise EOFError
            n = b[0]
            if not n: return result
            if n & 0xc0 == 0x80: n = ((n & 0x3f) << 8) | self.rfile.read(1)[0]
            elif n >= 0xc0: raise ValueError("fixture supports short words only")
            result.append(self.rfile.read(n).decode())

    def send(self, kind, tag, row=None):
        words = [kind] + ([tag] if tag else []) + [f"={k}={v}" for k, v in (row or {}).items()]
        self.wfile.write(b"".join(word(w) for w in words) + b"\0"); self.wfile.flush()

    def handle(self):
        try:
            while True:
                cmd = self.sentence()
                if not cmd: continue
                tag = next((w for w in cmd if w.startswith(".tag=")), "")
                rows = []
                elapsed = time.monotonic() - START
                if cmd[0] == "/ip/kid-control/device/print":
                    for i, (name, mac, ip, down, up) in enumerate(DEVICES):
                        row = {".id": f"*{i+1}", "name": name, "mac-address": mac,
                               "bytes-down": 2000000000 + int(elapsed*down),
                               "bytes-up": 300000000 + int(elapsed*up),
                               "rate-down": down*8, "rate-up": up*8}
                        rows.append(row)
                elif cmd[0] == "/ip/arp/print":
                    rows = [{"address": d[2], "mac-address": d[1]} for d in DEVICES]
                elif cmd[0] == "/system/resource/print":
                    rows = [{"uptime": "1d00:00:00", "version": "7.23-fixture", "board-name": "Synthetic",
                             "cpu-load": "4", "free-memory": "1073741824", "total-memory": "2147483648"}]
                elif cmd[0] != "/login" and cmd[0] != "/cancel" and not cmd[0].endswith("/print"):
                    self.send("!trap", tag, {"message": "read-only synthetic fixture"})
                for row in rows: self.send("!re", tag, row)
                self.send("!done", tag)
        except (EOFError, ConnectionError, ValueError): pass


class TCP(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


if __name__ == "__main__":
    tcp = TCP(("127.0.0.1", 19091), ROS)
    threading.Thread(target=tcp.serve_forever, daemon=True).start()
    print("Synthetic loopback fixtures: HTTP 19090, RouterOS API 19091", flush=True)
    http.server.ThreadingHTTPServer(("127.0.0.1", 19090), Controller).serve_forever()
