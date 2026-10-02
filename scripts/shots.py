#!/usr/bin/env python3
"""Capture the docs/*.png screenshots from the demo server.

Both CableDrop pages are `100dvh` roots wrapping an inner scroller, and that
shape defeats every simple approach:

1. **A plain `--screenshot` only captures the viewport.** The rest of the page
   lives inside the inner scroller, so it is never in frame.
2. **Growing `--window-size` does not fix it, it moves the cut.** The inner
   layout is viewport-dependent, so a taller window re-lays the page out at a
   size no device has — you get a stretched mock, not the real page. Past a
   point the extra viewport even comes back as a blank tail.
3. **`captureBeyondViewport` alone does nothing here**, because the *document*
   never overflows: the overflow belongs to an inner element, and the document
   keeps reporting a viewport-sized content box.

So: release the scrollers on the live page (a style override, capture-time
only — the app's own CSS is untouched), let the document grow to the real
content height, then shoot past the viewport. Each image is the whole page, at
a viewport the layout was actually designed for.

Needs Chrome and nothing else: the DevTools Protocol client below is stdlib.

Run the demo server first:
    CABLEDROP_SHOTS=1 go test -run TestScreenshotServer -timeout 1h ./internal/serve/
"""
import base64
import json
import os
import pathlib
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import time
import urllib.request

BASE = "http://127.0.0.1:18766"
CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
OUT = pathlib.Path(__file__).resolve().parent.parent / "docs"
SCALE = 2
# A fresh profile per run. Reusing a fixed path means one run that had to be
# killed leaves a profile the next run inherits — and the symptom is not a
# crash, it is every navigation landing on chrome-error://chromewebdata with
# screenshots that quietly come back viewport-sized and blank.
PROFILE = pathlib.Path(tempfile.mkdtemp(prefix="cabledrop-shots-"))


def free_port() -> int:
    """An unused local port for this run's DevTools socket.

    Fixed ports are a trap here: `adb forward tcp:9333` for inspecting a phone
    WebView takes the same number, Chrome then silently fails to bind, and the
    readiness poll below happily answers — from the phone. The run drives the
    wrong browser entirely and every screenshot comes back wrong without one
    error. Picking a free port removes the collision; the href check in
    capture() catches it if anything else ever goes wrong the same way.
    """
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]

# page -> (path, css width, css height, taller than the viewport?)
#
# The panel is a fixed 380x540 window that is laid out to fit exactly — its
# content is meant not to overflow, so a document that comes back exactly
# viewport-sized is the correct answer, not a failed capture. The phone page is
# the opposite: it scrolls inside an inner element and the whole point of the
# release step is to make it taller than the screen.
PAGES = {
    "panel": ("/panel", 380, 540, False),
    "phone": ("/", 390, 844, True),
}

# Release the page's own scroll plumbing so the document can grow to the real
# content height.
#
# Two things have to come apart, in this order. First the body, which this page
# pins to the viewport (position: fixed, all four offsets set) and scrolls
# inside an inner element — so the document never overflows and
# captureBeyondViewport has nothing to shoot. Then every inner scroller, whose
# overflow has to stop clipping before its content counts towards the height.
# A flex child needs `flex: none` as well: releasing `height` alone leaves
# `flex-basis: 0%` in charge, and the element stays viewport-sized.
UNCLIP = """(() => {
  const force = (el, prop, value) => el.style.setProperty(prop, value, 'important');
  let n = 0;

  const body = document.body;
  force(body, 'position', 'static');
  force(body, 'height', 'auto');
  force(body, 'max-height', 'none');
  force(body, 'overflow', 'visible');
  for (const p of ['top', 'right', 'bottom', 'left']) force(body, p, 'auto');

  const html = document.documentElement;
  force(html, 'height', 'auto');
  force(html, 'overflow', 'visible');

  document.querySelectorAll('*').forEach(el => {
    const cs = getComputedStyle(el);
    if (cs.overflowY === 'auto' || cs.overflowY === 'scroll' ||
        cs.overflow === 'auto' || cs.overflow === 'scroll') {
      force(el, 'overflow', 'visible');
      force(el, 'height', 'auto');
      force(el, 'max-height', 'none');
      force(el, 'flex', 'none');
      n++;
    }
  });
  return n;
})()"""

# The real bottom/right edge. getLayoutMetrics keeps reporting a viewport-sized
# content box, so ask the page instead.
MEASURE = """(() => {
  const de = document.documentElement;
  let bottom = Math.max(de.scrollHeight, document.body.scrollHeight);
  let right = Math.max(de.scrollWidth, document.body.scrollWidth);
  document.querySelectorAll('body *').forEach(el => {
    const r = el.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) {
      bottom = Math.max(bottom, r.bottom + window.scrollY);
      right = Math.max(right, r.right + window.scrollX);
    }
  });
  return JSON.stringify([Math.ceil(right), Math.ceil(bottom)]);
})()"""


class WS:
    """Just enough of RFC 6455 to talk to one DevTools target."""

    # The handshake key only has to be 16 random bytes in base64 and come back
    # in the response; a fixed one is fine for a loopback DevTools socket and
    # keeps this file free of a random-bytes dependency.
    KEY = "dGhlIHNhbXBsZSBub25jZQ=="

    def __init__(self, url, timeout=120):
        if not url.startswith("ws://"):
            raise ValueError(f"not a ws url: {url}")
        hostport, _, path = url[len("ws://"):].partition("/")
        host, _, port = hostport.partition(":")
        self.sock = socket.create_connection((host, int(port)), timeout=timeout)
        self.sock.settimeout(timeout)
        self.sock.sendall(
            f"GET /{path} HTTP/1.1\r\nHost: {hostport}\r\n"
            "Upgrade: websocket\r\nConnection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {self.KEY}\r\nSec-WebSocket-Version: 13\r\n\r\n".encode())
        buf = b""
        while b"\r\n\r\n" not in buf:
            chunk = self.sock.recv(4096)
            if not chunk:
                raise RuntimeError("DevTools handshake closed early")
            buf += chunk
        head, _, self.buf = buf.partition(b"\r\n\r\n")
        if b"101" not in head.split(b"\r\n")[0]:
            raise RuntimeError(f"DevTools handshake failed: {head!r}")

    def _need(self, n):
        while len(self.buf) < n:
            chunk = self.sock.recv(1 << 16)
            if not chunk:
                raise RuntimeError("DevTools connection closed")
            self.buf += chunk
        out, self.buf = self.buf[:n], self.buf[n:]
        return out

    def _frame(self, opcode, data):
        header = bytearray([0x80 | opcode])
        n = len(data)
        if n < 126:
            header.append(0x80 | n)
        elif n < 1 << 16:
            header.append(0x80 | 126)
            header += struct.pack(">H", n)
        else:
            header.append(0x80 | 127)
            header += struct.pack(">Q", n)
        mask = os.urandom(4)
        header += mask
        self.sock.sendall(bytes(header)
                          + bytes(b ^ mask[i % 4] for i, b in enumerate(data)))

    def send(self, text):
        self._frame(0x1, text.encode())

    def recv(self):
        payload = b""
        while True:
            b1, b2 = self._need(2)
            fin, opcode = b1 & 0x80, b1 & 0x0F
            length = b2 & 0x7F
            if length == 126:
                length = struct.unpack(">H", self._need(2))[0]
            elif length == 127:
                length = struct.unpack(">Q", self._need(8))[0]
            mask = self._need(4) if b2 & 0x80 else None
            data = self._need(length)
            if mask:
                data = bytes(b ^ mask[i % 4] for i, b in enumerate(data))
            if opcode == 0x9:                 # ping -> pong
                self._frame(0xA, data)
                continue
            if opcode == 0xA:                 # pong
                continue
            if opcode == 0x8:                 # close
                raise RuntimeError("DevTools closed the connection")
            payload += data                   # 0x0 continuation: keep appending
            if fin:
                return payload.decode("utf-8", "replace")

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass


class CDP:
    """One-request-at-a-time DevTools client."""

    def __init__(self, ws):
        self.ws = ws
        self._id = 0

    def __call__(self, method, **params):
        self._id += 1
        self.ws.send(json.dumps({"id": self._id, "method": method, "params": params}))
        while True:
            msg = json.loads(self.ws.recv())
            if msg.get("id") == self._id:
                if "error" in msg:
                    raise RuntimeError(f"{method}: {msg['error']}")
                return msg.get("result", {})

    def js(self, expression):
        res = self("Runtime.evaluate", expression=expression, returnByValue=True)
        if "exceptionDetails" in res:
            raise RuntimeError(f"page threw: {res['exceptionDetails']}")
        return res["result"].get("value")


def start_chrome(port: int) -> subprocess.Popen:
    proc = subprocess.Popen(
        [CHROME, "--headless", "--disable-gpu", "--hide-scrollbars",
         "--no-first-run", "--no-default-browser-check",
         f"--remote-debugging-port={port}", "--remote-allow-origins=*",
         f"--user-data-dir={PROFILE}", "--window-size=390,844", "about:blank"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    deadline = time.time() + 30
    while time.time() < deadline:
        try:
            urllib.request.urlopen(
                f"http://127.0.0.1:{port}/json/version", timeout=1).read()
            return proc
        except OSError:
            time.sleep(0.3)
    proc.kill()
    raise RuntimeError(f"Chrome never opened a DevTools port on {port}")


def capture(cdp: CDP, url: str, w: int, h: int, out: pathlib.Path, dark: bool,
            taller: bool) -> None:
    cdp("Emulation.setDeviceMetricsOverride",
        width=w, height=h, deviceScaleFactor=SCALE, mobile=w < 500)
    cdp("Emulation.setEmulatedMedia", media="",
        features=[{"name": "prefers-color-scheme",
                   "value": "dark" if dark else "light"}])
    cdp("Page.navigate", url=url)
    time.sleep(2.5)                                    # load + first API poll
    cdp("Runtime.evaluate", expression="new Promise(r => setTimeout(r, 1200))",
        awaitPromise=True)

    # cdp.js, not a bare Runtime.evaluate: this returns a value, and the value
    # is worth printing — if the page's own JS has rebuilt a subtree in the
    # meantime, the release silently does nothing and the capture quietly comes
    # back viewport-sized.
    released = cdp.js(UNCLIP)
    time.sleep(0.5)
    where = json.loads(cdp.js(
        "JSON.stringify({href: location.href, title: document.title,"
        " cards: document.querySelectorAll('.p-card').length,"
        " scrolls: [...document.querySelectorAll('*')]"
        ".filter(e => ['auto','scroll'].includes(getComputedStyle(e).overflowY)).length})"))
    if not where["href"].startswith(BASE):
        raise RuntimeError(
            f"the page never loaded: landed on {where['href']} ({where['title']}). "
            f"Is the demo server up on {BASE}?")
    cw, ch = json.loads(cdp.js(MEASURE))
    if taller and ch <= h:
        raise RuntimeError(
            f"document is {cw}x{ch}, no taller than the {w}x{h} viewport — the "
            f"release step ({released} elements) did not take, so this image "
            f"would be the first screen only")

    shot = cdp("Page.captureScreenshot", format="png", captureBeyondViewport=True,
               clip={"x": 0, "y": 0, "width": cw, "height": ch, "scale": 1})
    out.write_bytes(base64.b64decode(shot["data"]))
    print(f"  {out.name}: {cw}x{ch} css -> {cw * SCALE}x{ch * SCALE}px")


def main() -> int:
    OUT.mkdir(parents=True, exist_ok=True)
    port = free_port()
    proc = start_chrome(port)
    try:
        targets = json.load(urllib.request.urlopen(
            f"http://127.0.0.1:{port}/json"))
        page = next(t for t in targets if t["type"] == "page")
        ws = WS(page["webSocketDebuggerUrl"])
        cdp = CDP(ws)
        cdp("Page.enable")
        for name, (path, w, h, taller) in PAGES.items():
            for theme, dark in (("light", False), ("dark", True)):
                capture(cdp, BASE + path, w, h, OUT / f"{name}-{theme}.png", dark,
                        taller)
        ws.close()
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
        shutil.rmtree(PROFILE, ignore_errors=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
