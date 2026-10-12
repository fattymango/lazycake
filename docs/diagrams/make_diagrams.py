#!/usr/bin/env python3
"""Generates the architecture diagrams as Excalidraw files (open them at https://excalidraw.com, File > Open, or drag them in):

    python3 docs/diagrams/make_diagrams.py            # writes docs/diagrams/lazycake-architecture.excalidraw (all three side by side; --separate also writes one file each)
    ./docs/diagrams/export.sh                          # optional: PNG + SVG next to them (needs the one-time setup described in export.sh)

Editing: change the layout here and regenerate, or edit the .excalidraw files by hand in Excalidraw (every card is a group: box + title + text).
"""
import json
import math
import os
import random

HERE = os.path.dirname(os.path.abspath(__file__))
FONT = 1  # Virgil (hand-drawn)

# Excalidraw's own palette: (fill, stroke)
BLUE = ("#a5d8ff", "#1971c2")
GREEN = ("#b2f2bb", "#2f9e44")
ORANGE = ("#ffd8a8", "#e8590c")
YELLOW = ("#ffec99", "#f08c00")
PURPLE = ("#d0bfff", "#6741d9")
TEAL = ("#96f2d7", "#099268")
RED = ("#ffc9c9", "#e03131")
GRAY = ("#e9ecef", "#495057")
INK = "#1e1e1e"
MUTED = "#495057"


def col(c):
    """A colour may be given as a (fill, stroke) pair; lines and text use the stroke."""
    return c[1] if isinstance(c, tuple) else c


class Scene:
    def __init__(self, seed=7):
        self.els = []
        self.rnd = random.Random(seed)
        self.n = 0
        self.byid = {}

    def _base(self, kind, x, y, w, h, **kw):
        self.n += 1
        eid = kw.pop("id", f"e{self.n}")
        el = {
            "id": eid, "type": kind, "x": x, "y": y, "width": w, "height": h, "angle": 0,
            "strokeColor": col(kw.pop("stroke", INK)), "backgroundColor": kw.pop("bg", "transparent"),
            "fillStyle": kw.pop("fill", "solid"), "strokeWidth": kw.pop("sw", 2), "strokeStyle": kw.pop("style", "solid"),
            "roughness": kw.pop("rough", 1), "opacity": kw.pop("opacity", 100), "groupIds": kw.pop("groups", []), "frameId": None,
            "roundness": kw.pop("roundness", None), "seed": self.rnd.randint(1, 2**31 - 1), "version": 1, "versionNonce": self.rnd.randint(1, 2**31 - 1),
            "isDeleted": False, "boundElements": [], "updated": 1, "link": None, "locked": False,
        }
        el.update(kw)
        self.els.append(el)
        self.byid[eid] = el
        return el

    # ---- text -----------------------------------------------------------------------------------------------------------------
    @staticmethod
    def measure(text, fs):
        lines = text.split("\n")
        return max(len(l) for l in lines) * fs * 0.56, len(lines) * fs * 1.25

    def text(self, x, y, text, fs=18, color=INK, align="left", groups=None, w=None):
        tw, th = self.measure(text, fs)
        w = w or tw
        if align == "center":
            x = x - w / 2  # x is the centre
        elif align == "right":
            x = x - w
        return self._base("text", x, y, w, th, stroke=color, groups=groups or [], text=text, originalText=text, fontSize=fs, fontFamily=FONT,
                          textAlign=align, verticalAlign="top", containerId=None, autoResize=True, lineHeight=1.25, rough=1, sw=1)

    # ---- shapes ---------------------------------------------------------------------------------------------------------------
    def card(self, x, y, w, h, title, body="", color=BLUE, tfs=22, bfs=16, style="solid", bg=None, round_=True, sw=2):
        gid = f"g{self.n + 1}"
        fill, stroke = color
        rect = self._base("rectangle", x, y, w, h, stroke=stroke, bg=bg or fill, style=style, groups=[gid], sw=sw, roundness={"type": 3} if round_ else None)
        th = tfs * 1.25 if title else 0
        bh = len(body.split("\n")) * bfs * 1.25 if body else 0
        gap = 6 if title and body else 0
        top = y + (h - (th + gap + bh)) / 2
        for t, fs, yy, c in ((title, tfs, top, INK), (body, bfs, top + th + gap, MUTED if title else INK)):
            if t:
                tw, _ = self.measure(t, fs)
                if tw > w - 14:
                    print(f"  ! text may overflow its card ({tw:.0f} > {w - 14}): {t.splitlines()[0][:40]!r}")
                self.text(x + w / 2, yy, t, fs=fs, color=c, align="center", groups=[gid], w=w - 10)
        return rect["id"]

    def zone(self, x, y, w, h, label, color=GRAY, style="dashed", lfs=20, tint="#f8f9fa", label_color=None):
        """A big container drawn behind a group of cards."""
        r = self._base("rectangle", x, y, w, h, stroke=color[1], bg=tint, style=style, sw=2, roundness={"type": 3})
        self.els.remove(r)
        self.els.insert(0, r)  # behind everything drawn so far
        self.text(x + 18, y + 12, label, fs=lfs, color=label_color or color[1])
        return r["id"]

    def bubble(self, cx, cy, num, color=RED, r=19):
        e = self._base("ellipse", cx - r, cy - r, 2 * r, 2 * r, stroke=color[1], bg=color[0], sw=2, roundness={"type": 2})
        t = self._base("text", cx - 7, cy - 12, 14, 25, stroke=INK, text=str(num), originalText=str(num), fontSize=20, fontFamily=FONT, textAlign="center",
                       verticalAlign="middle", containerId=e["id"], autoResize=True, lineHeight=1.25, sw=1)
        e["boundElements"].append({"type": "text", "id": t["id"]})
        return e["id"]

    # ---- arrows ---------------------------------------------------------------------------------------------------------------
    def _anchor(self, eid, side, off=0.0):
        e = self.byid[eid]
        x, y, w, h = e["x"], e["y"], e["width"], e["height"]
        return {"l": (x, y + h / 2 + off), "r": (x + w, y + h / 2 + off), "t": (x + w / 2 + off, y), "b": (x + w / 2 + off, y + h)}[side]

    def arrow(self, a, b, a_side="r", b_side="l", via=(), color=INK, style="solid", both=False, sw=2, a_off=0.0, b_off=0.0):
        """Arrow from shape a to shape b (ids), optionally through absolute waypoints (elbows). Bound to both shapes, so it follows them in Excalidraw."""
        p0 = self._anchor(a, a_side, a_off)
        p1 = self._anchor(b, b_side, b_off)
        pts = [p0, *via, p1]
        gap = 6

        def nudge(p, q, g):
            dx, dy = q[0] - p[0], q[1] - p[1]
            d = math.hypot(dx, dy) or 1
            return (p[0] + dx / d * g, p[1] + dy / d * g)

        pts[0] = nudge(pts[0], pts[1], gap)
        pts[-1] = nudge(pts[-1], pts[-2], gap)
        xs, ys = [p[0] for p in pts], [p[1] for p in pts]
        el = self._base("arrow", pts[0][0], pts[0][1], max(xs) - min(xs), max(ys) - min(ys), stroke=color, style=style, sw=sw, roundness=None,
                        points=[[p[0] - pts[0][0], p[1] - pts[0][1]] for p in pts], lastCommittedPoint=None, elbowed=False,
                        startBinding={"elementId": a, "focus": 0, "gap": gap}, endBinding={"elementId": b, "focus": 0, "gap": gap},
                        startArrowhead="arrow" if both else None, endArrowhead="arrow")
        self.byid[a]["boundElements"].append({"type": "arrow", "id": el["id"]})
        self.byid[b]["boundElements"].append({"type": "arrow", "id": el["id"]})
        return el["id"]

    def free_arrow(self, p0, p1, via=(), color=INK, style="solid", sw=2, both=False):
        """An arrow not tied to any shape (legend samples, the encrypted-session bar)."""
        pts = [p0, *via, p1]
        xs, ys = [p[0] for p in pts], [p[1] for p in pts]
        return self._base("arrow", p0[0], p0[1], max(xs) - min(xs), max(ys) - min(ys), stroke=color, style=style, sw=sw, roundness=None,
                          points=[[p[0] - p0[0], p[1] - p0[1]] for p in pts], lastCommittedPoint=None, elbowed=False, startBinding=None, endBinding=None,
                          startArrowhead="arrow" if both else None, endArrowhead="arrow")["id"]

    def save(self, name):
        doc = {"type": "excalidraw", "version": 2, "source": "https://excalidraw.com", "elements": self.els,
               "appState": {"gridSize": None, "viewBackgroundColor": "#ffffff"}, "files": {}}
        path = os.path.join(HERE, name)
        with open(path, "w") as f:
            json.dump(doc, f, indent=1)
        print("wrote", path, f"({len(self.els)} elements)")


# =============================================================================================================================
# 1. Overview (portrait: customers on top, the coordinator in the middle, gateway and providers at the bottom)
# =============================================================================================================================
def overview():
    s = Scene(seed=11)
    s.text(40, 28, "LazyCake", fs=56, color=INK)
    s.text(46, 100, "a marketplace for leftover CPU", fs=26, color=MUTED)
    s.text(1460, 112, "everything dials OUT, nothing opens inbound", fs=20, color=PURPLE[1], align="right")

    # customers
    s.zone(40, 190, 1420, 250, "Customers", color=BLUE, tint="#f3f9ff", lfs=24)
    dash = s.card(80, 250, 400, 150, "Dashboard", "https", BLUE, tfs=30, bfs=20)
    ai = s.card(520, 250, 400, 150, "AI apps", "MCP + OAuth", PURPLE, tfs=30, bfs=20)
    cli = s.card(960, 250, 420, 150, "CLI", "gRPC + TLS", BLUE, tfs=30, bfs=20)

    # coordinator
    s.zone(40, 490, 1420, 640, "Coordinator", color=PURPLE, style="solid", tint="#f6f3ff", lfs=26)
    s.text(1440, 502, "one Go binary", fs=20, color=PURPLE[1], align="right")
    api = s.card(80, 560, 840, 170, "Portal API + dashboard", "https :8080  ·  MCP  ·  OAuth", BLUE, tfs=30, bfs=20)
    grpc = s.card(960, 560, 420, 170, "gRPC :7443", "TLS  ·  agents + CLI", GREEN, tfs=30, bfs=20)
    sch = s.card(80, 770, 400, 150, "Scheduler", "trust  ·  placement", ORANGE, tfs=30, bfs=20)
    bil = s.card(520, 770, 400, 150, "Billing", "per-second ledger", YELLOW, tfs=30, bfs=20)
    pgs = s.card(960, 770, 420, 150, "PostgreSQL", "all state", GRAY, tfs=30, bfs=20)
    rel = s.card(80, 960, 1300, 120, "Tunnel relay  ·  QUIC :7444", "byte-blind: encrypted end to end inside", TEAL, tfs=30, bfs=20)

    # customer's private network
    s.zone(40, 1170, 680, 560, "Customer's private network", color=YELLOW, tint="#fffdf0", lfs=24)
    gw = s.card(80, 1250, 600, 140, "Gateway", "forwards only what is published", YELLOW, tfs=30, bfs=20)
    pg = s.card(80, 1470, 180, 120, "Postgres", ":5432", GRAY, tfs=24, bfs=18)
    fl = s.card(290, 1470, 180, 120, "files", ":8000", GRAY, tfs=24, bfs=18)
    mt = s.card(500, 1470, 180, 120, "metrics", ":9100", GRAY, tfs=24, bfs=18)
    for t in (pg, fl, mt):
        s.arrow(gw, t, "b", "t", color=MUTED, sw=2)
    s.text(380, 1630, "never open to the internet", fs=20, color=MUTED, align="center")

    # providers
    s.zone(760, 1170, 700, 560, "Providers", color=GREEN, tint="#f4fcf5", lfs=24)
    ag = s.card(800, 1250, 580, 140, "Agent", "a container on any PC or server", GREEN, tfs=30, bfs=20)
    s.zone(800, 1415, 580, 190, "sandbox", color=GRAY, tint="#ffffff", lfs=20, style="solid")
    s.card(830, 1465, 250, 115, "Task", "CPU + RAM caps", GRAY, tfs=26, bfs=18, bg="#fff3bf")
    s.card(1100, 1465, 250, 115, "Task", "no inbound network", GRAY, tfs=26, bfs=18, bg="#fff3bf")
    s.card(800, 1630, 580, 70, "Machine 2 ... N", "", GREEN, tfs=24, style="dashed")

    # arrows: customers in, everything else dials out
    s.arrow(dash, api, "b", "t", color=BLUE[1], sw=3, b_off=-220)
    s.arrow(ai, api, "b", "t", color=PURPLE[1], sw=3, b_off=220)
    s.arrow(cli, grpc, "b", "t", color=BLUE[1], sw=3)
    s.arrow(gw, rel, "t", "b", color=YELLOW[1], sw=4, style="dashed", a_off=180, b_off=-170)
    s.text(580, 1195, "dials out", fs=20, color="#b36b00")
    s.arrow(ag, rel, "t", "b", color=TEAL[1], sw=4, style="dashed", b_off=370)
    s.text(1118, 1195, "dials out", fs=20, color=TEAL[1])
    s.arrow(ag, grpc, "r", "r", via=((1420, 1320), (1420, 645)), color=GREEN[1], sw=4)
    return s


# =============================================================================================================================
# 2. One task, using a gateway and a remote machine (portrait zigzag: across the top, down the right, back along the bottom)
# =============================================================================================================================
def task_flow():
    s = Scene(seed=23)
    s.text(40, 28, "One task, start to finish", fs=52, color=INK)
    s.text(46, 98, "a remote machine reaching a private database", fs=26, color=MUTED)

    # top row: customer -> coordinator
    s.zone(40, 170, 560, 350, "Customer", color=BLUE, tint="#f3f9ff", lfs=24)
    bal = s.card(80, 235, 480, 90, "Balance", "pays per second used", GRAY, tfs=26, bfs=18)
    cust = s.card(80, 355, 480, 130, "Customer", "\"run my job, reach my database\"", BLUE, tfs=30, bfs=18)
    s.zone(660, 170, 800, 350, "Coordinator", color=PURPLE, style="solid", tint="#f6f3ff", lfs=24)
    api = s.card(690, 355, 240, 130, "API", "check, hold $", BLUE, tfs=28, bfs=18)
    sch = s.card(950, 355, 230, 130, "Scheduler", "pick a machine", ORANGE, tfs=28, bfs=18)
    bil = s.card(1200, 355, 210, 130, "Billing", "settle + trust", YELLOW, tfs=28, bfs=18)

    # provider's machine (right column, going down)
    s.zone(660, 560, 800, 640, "Provider's machine", color=GREEN, tint="#f4fcf5", lfs=24)
    ag = s.card(700, 630, 720, 110, "Agent", "one stream to the coordinator", GREEN, tfs=30, bfs=18)
    s.zone(700, 775, 720, 395, "sandbox", color=GRAY, tint="#ffffff", lfs=20, style="solid")
    task = s.card(740, 835, 640, 120, "Task container", "python etl.run  ·  2 vCPU, 2 GB", GRAY, tfs=28, bfs=18, bg="#fff3bf")
    prx = s.card(740, 1005, 640, 120, "Tunnel proxy", "postgres:5432  ->  tunnel", TEAL, tfs=28, bfs=18)

    # the story, short (left column)
    s.zone(40, 560, 560, 640, "What happens", color=GRAY, style="solid", tint="#ffffff", lfs=28, label_color=INK)
    steps = ["Customer submits the task", "Scheduler picks a machine", "Agent starts it in a sandbox", "It opens postgres:5432",
             "Relay passes bytes it can't read", "Gateway reaches the database", "Logs and usage stream back", "Billed per second, provider paid"]
    for i, t in enumerate(steps):
        yy = 640 + i * 66
        s.bubble(85, yy + 18, i + 1, r=22)
        s.text(125, yy + 2, t, fs=24, color=INK)

    # bottom row: relay (right) -> gateway -> database (left)
    s.zone(780, 1230, 680, 270, "Tunnel relay", color=TEAL, tint="#f1fffb", lfs=24)
    rel = s.card(820, 1290, 600, 150, "QUIC :7444", "part of the coordinator, can't read the traffic", TEAL, tfs=30, bfs=18)
    s.zone(40, 1230, 720, 470, "Customer's private network", color=YELLOW, tint="#fffdf0", lfs=24)
    gw = s.card(300, 1290, 420, 150, "Gateway", "dialed out, forwards one service", YELLOW, tfs=30, bfs=18)
    pg = s.card(300, 1520, 420, 130, "Postgres", "the customer's database", GRAY, tfs=30, bfs=18)
    s.text(110, 1330, "no inbound\nport open", fs=20, color="#b36b00", align="center")

    # numbered flow
    s.arrow(cust, api, "r", "l", color=BLUE[1], sw=4)
    s.bubble(630, 400, 1)
    s.arrow(sch, ag, "b", "t", color=ORANGE[1], sw=4)
    s.bubble(1066, 560, 2)
    s.arrow(ag, task, "b", "t", color=GREEN[1], sw=4)
    s.bubble(1066, 790, 3)
    s.arrow(task, prx, "b", "t", color=TEAL[1], sw=4)
    s.arrow(prx, rel, "b", "t", color=TEAL[1], sw=4, style="dashed", b_off=0)
    s.bubble(1066, 1205, 4)
    s.arrow(rel, gw, "l", "r", color=TEAL[1], sw=4, style="dashed")
    s.bubble(770, 1330, 5)
    s.arrow(gw, pg, "b", "t", color=YELLOW[1], sw=4)
    s.bubble(510, 1480, 6)
    s.arrow(ag, bil, "r", "r", via=((1440, 685), (1440, 420)), color=GREEN[1], sw=3, style="dashed")
    s.bubble(1440, 560, 7)
    s.arrow(bil, bal, "t", "r", via=((1305, 280), (590, 280)), color=YELLOW[1], sw=3, style="dashed", a_off=0)
    s.bubble(840, 280, 8)
    s.text(1066, 1450, "encrypted end to end (Noise)", fs=20, color=PURPLE[1], align="center")
    s.card(780, 1530, 680, 170, "No inbound port, anywhere", "not on the provider's network,\nnot on the customer's", PURPLE, tfs=32, bfs=22, style="dashed")
    return s


# =============================================================================================================================
# 3. How a gateway works: registration, finding the database by name (DNS), and one connection
# =============================================================================================================================
def gateway_explained():
    s = Scene(seed=37)
    s.text(40, 28, "How a gateway works", fs=52, color=INK)
    s.text(46, 98, "registering it, finding the database by name, using it", fs=26, color=MUTED)

    # ---- 1. register ------------------------------------------------------------------------------------------------
    s.zone(40, 170, 1420, 520, "1   Register a gateway (once)", color=YELLOW, tint="#fffdf0", lfs=26)
    a1 = s.card(80, 250, 290, 150, "Create", "in the dashboard:\nname + services", BLUE, tfs=30, bfs=19)
    a2 = s.card(450, 250, 290, 150, "Token", "shown once,\nstored hashed", PURPLE, tfs=30, bfs=19)
    a3 = s.card(830, 250, 270, 150, "Start", "the gateway makes\nits own Noise key", YELLOW, tfs=30, bfs=19)
    a4 = s.card(1180, 250, 240, 150, "Register", "token + public key", TEAL, tfs=30, bfs=19)
    s.arrow(a1, a2, "r", "l", color=BLUE[1], sw=4)
    s.arrow(a2, a3, "r", "l", color=PURPLE[1], sw=4)
    s.text(785, 262, "install\ncommand", fs=17, color=PURPLE[1], align="center")
    s.arrow(a3, a4, "r", "l", color=YELLOW[1], sw=4, style="dashed")
    s.text(1140, 262, "dials\nOUT", fs=17, color="#b36b00", align="center")
    s.card(80, 470, 660, 150, "Its Noise key", "the private half never leaves the gateway", PURPLE, tfs=30, bfs=20, style="dashed")
    a6 = s.card(830, 470, 270, 150, "Hello", "version + speed limit", GREEN, tfs=30, bfs=19)
    a7 = s.card(1180, 470, 240, 150, "Online", "in the dashboard", GREEN, tfs=30, bfs=19)
    s.arrow(a3, a6, "b", "t", color=GREEN[1], sw=3)
    s.arrow(a6, a7, "r", "l", color=GREEN[1], sw=3)
    s.arrow(a4, a7, "b", "t", color=TEAL[1], sw=3)
    s.text(750, 640, "tasks later get the public key in their dispatch, so only this gateway can read what they send", fs=19, color=MUTED, align="center")

    # ---- 2. DNS -----------------------------------------------------------------------------------------------------
    s.zone(40, 730, 1420, 560, "2   Find the database by name (DNS, inside the task's sandbox)", color=GREEN, tint="#f4fcf5", lfs=26)
    b1 = s.card(80, 810, 500, 120, "Task", "connect(\"postgres\", 5432)", GRAY, tfs=30, bfs=20, bg="#fff3bf")
    b2 = s.card(80, 1000, 500, 120, "Stub DNS", "answers only declared names", TEAL, tfs=30, bfs=20)
    b3 = s.card(760, 1000, 640, 120, "Tunnel proxy", "listens on 127.0.10.2:5432", TEAL, tfs=30, bfs=20)
    s.arrow(b1, b2, "b", "t", color=TEAL[1], sw=4)
    s.text(345, 950, "who is postgres?", fs=20, color=TEAL[1])
    s.arrow(b2, b3, "r", "l", color=TEAL[1], sw=4)
    s.text(670, 1030, "127.0.10.2", fs=20, color=TEAL[1], align="center")
    s.arrow(b1, b3, "r", "t", via=((1080, 870),), color=GREEN[1], sw=4, style="dashed")
    s.text(830, 836, "then connects to that address", fs=20, color=GREEN[1])
    s.card(80, 1165, 1320, 100, "Any other name", "NXDOMAIN: the task has no route at all", RED, tfs=28, bfs=20, style="dashed")

    # ---- 3. one connection ------------------------------------------------------------------------------------------
    s.zone(40, 1330, 1420, 550, "3   Use it (every connection)", color=TEAL, tint="#f1fffb", lfs=26)
    c1 = s.card(80, 1410, 275, 150, "Tunnel proxy", "one stream per\nconnection", TEAL, tfs=28, bfs=18)
    c2 = s.card(435, 1410, 275, 150, "Relay", "pairs two streams,\ncan't read them", TEAL, tfs=28, bfs=18)
    c3 = s.card(790, 1410, 275, 150, "Gateway", "ends the\nencryption", YELLOW, tfs=28, bfs=18)
    c4 = s.card(1145, 1410, 275, 150, "Postgres", "127.0.0.1:5432", GRAY, tfs=28, bfs=18)
    s.arrow(c1, c2, "r", "l", color=TEAL[1], sw=4, style="dashed")
    s.arrow(c2, c3, "r", "l", color=TEAL[1], sw=4, style="dashed")
    s.arrow(c3, c4, "r", "l", color=YELLOW[1], sw=4)
    s.free_arrow((150, 1565), (830, 1565), via=((150, 1625), (830, 1625)), color=PURPLE[1], sw=2, style="dashed", both=True)
    s.text(505, 1633, "encrypted end to end (Noise): the relay can't read it", fs=19, color=PURPLE[1], align="center")
    g1 = s.card(435, 1710, 480, 130, "In the published list", "forwarded to 127.0.0.1:5432", GREEN, tfs=26, bfs=19)
    g2 = s.card(960, 1710, 460, 130, "Not in the list", "refused, nothing leaves", RED, tfs=26, bfs=19)
    s.arrow(c3, g1, "b", "t", color=GREEN[1], sw=3, a_off=-10)
    s.arrow(c3, g2, "b", "t", color=RED[1], sw=3, a_off=80)
    s.text(80, 1730, "the service name travels\ninside the encrypted\nstream; the gateway checks\nit against its list", fs=19, color=MUTED)
    return s


PITCH = 1580  # distance between the left edges of neighbouring diagrams (each is about 1500 wide)


def combine(scenes, pitch=PITCH):
    """Puts several scenes side by side in one drawing, with a vertical line between neighbours. Each scene keeps its own title at the top."""
    out = Scene(seed=1)
    bottom = 0
    for k, sc in enumerate(scenes):
        dx = k * pitch
        for el in sc.els:
            e = json.loads(json.dumps(el))  # deep copy
            pre = f"d{k}_"
            e["id"] = pre + e["id"]
            e["x"] += dx
            e["groupIds"] = [pre + g for g in e.get("groupIds", [])]
            if e.get("containerId"):
                e["containerId"] = pre + e["containerId"]
            e["boundElements"] = [dict(b, id=pre + b["id"]) for b in (e.get("boundElements") or [])]
            for side in ("startBinding", "endBinding"):
                if e.get(side):
                    e[side]["elementId"] = pre + e[side]["elementId"]
            out.els.append(e)
            bottom = max(bottom, e["y"] + e["height"])
    height = bottom + 20
    for k in range(1, len(scenes)):
        x = k * pitch - (pitch - 1500) / 2 + 20  # midway between the two neighbours
        line = out._base("line", x, 20, 0, height - 20, stroke="#868e96", sw=3, roundness=None,
                         points=[[0, 0], [0, height - 20]], lastCommittedPoint=None, startBinding=None, endBinding=None,
                         startArrowhead=None, endArrowhead=None)
        out.els.remove(line)
        out.els.insert(0, line)  # behind everything
    return out


if __name__ == "__main__":
    import sys

    parts = [overview(), task_flow(), gateway_explained()]
    combine(parts).save("lazycake-architecture.excalidraw")
    if "--separate" in sys.argv:  # also one file per drawing
        for name, sc in zip(("lazycake-architecture-overview", "lazycake-task-flow", "lazycake-gateway-explained"), parts):
            sc.save(name + ".excalidraw")
