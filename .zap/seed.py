"""Makes an owner, a gateway and a plug on the stack under test and writes the requests that ZAP replays.

The JSON API has no links for the ZAP spider to follow, so this is how ZAP gets to see real responses. It writes the file that the
first argument names. Every item has a method and a path, and may have `cookie`, `bearer`, `body` and `app`. An item with `app` set
is a request of the web app, so the replay adds the CSRF header and the origin that the API demands for those. The file holds the
session and the key of throwaway accounts, so it is not committed and is not uploaded. It only uses the standard library.

The passive baseline scan fails on any warning, so it only gets a short list. ZAP_FULL=true adds the routes that write and read the
rest of the API, which the active scan attacks.

Settings come from the environment
  BASE_URL      address of the API without a path, http://127.0.0.1:8080 by default, plain http for 127.0.0.1 only
  APP_ORIGIN    the origin that the API allows, http://localhost:5173 by default
  SMOKE_LOG_CMD a command that prints the logs of the API, where a stack with no mail server writes the verification link
  ZAP_FULL      true for the long list of the active scan
"""

import hashlib
import json
import os
import secrets
import shlex
import subprocess
import sys
import time
import urllib.error
import urllib.request
from urllib.parse import parse_qs, urlsplit

BASE = os.environ.get("BASE_URL", "http://127.0.0.1:8080").rstrip("/")
ORIGIN = os.environ.get("APP_ORIGIN", "http://localhost:5173")
LOG_CMD = os.environ.get("SMOKE_LOG_CMD", "")
FULL = os.environ.get("ZAP_FULL", "false") == "true"
RUN = time.strftime("%m%d%H%M%S")
EMAIL = f"zap-{RUN}@example.com"
PASSWORD = "Zap-" + secrets.token_urlsafe(18) + "1!"

URL = urlsplit(BASE)
LOCAL = URL.scheme == "http" and URL.hostname == "127.0.0.1" and URL.username is None and not URL.path.strip("/")

if len(sys.argv) != 2 or not LOCAL or not LOG_CMD:
    sys.exit("usage seed.py <output file>, with BASE_URL set to http://127.0.0.1 and SMOKE_LOG_CMD set")

OUT = sys.argv[1]


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


OPENER = urllib.request.build_opener(NoRedirect)


def call(method, path, body=None, cookie="", bearer="", app=True):
    """Sends one request and returns the status, the headers and the JSON body"""
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")

    if app:
        req.add_header("X-Requested-With", "aethera")
        req.add_header("Origin", ORIGIN)

    if cookie:
        req.add_header("Cookie", cookie)

    if bearer:
        req.add_header("Authorization", "Bearer " + bearer)

    try:
        with OPENER.open(req, timeout=20) as res:
            status, headers, raw = res.status, res.headers, res.read()
    except urllib.error.HTTPError as err:
        status, headers, raw = err.code, err.headers, err.read()

    try:
        return status, headers, json.loads(raw)
    except ValueError:
        return status, headers, {}


def need(label, answer, status):
    """Stops the run when a step of the set up does not answer as expected, because the scan would only fail later and with less to read"""
    if answer[0] != status:
        sys.exit(f"{label} answered {answer[0]} and not {status}")

    return answer[2]


def verification_link():
    for _ in range(20):
        out = subprocess.run(shlex.split(LOG_CMD), capture_output=True, text=True, check=False).stdout

        for line in reversed(out.splitlines()):
            start = line.find("{")
            if start < 0 or "verification email" not in line:
                continue

            try:
                entry = json.loads(line[start:])
            except ValueError:
                continue

            if entry.get("to") == EMAIL:
                return entry.get("link", "")

        time.sleep(1)

    return ""


need("sign up", call("POST", "/v1/auth/register", {"businessName": "Toko ZAP", "email": EMAIL, "password": PASSWORD}), 201)
token = (parse_qs(urlsplit(verification_link()).query).get("token") or [""])[0]
if not token:
    sys.exit("the verification link did not reach the log")

status, headers, _ = call("POST", "/v1/auth/verify-email", {"token": token})
cookie = headers.get("Set-Cookie", "").split(";", 1)[0]
if status != 200 or "=" not in cookie:
    sys.exit(f"verify the email answered {status}")

room = need("make a room", call("POST", "/v1/rooms", {"name": "Ruang ZAP"}, cookie=cookie), 201)["id"]
key = secrets.token_urlsafe(32)
enroll = need("enroll", call("POST", "/device/v1/enroll", {
    "hardwareId": f"ZAP-{RUN}", "firmware": "zap", "keyHash": hashlib.sha256(key.encode()).hexdigest(),
}, app=False), 201)
need("claim", call("POST", "/v1/gateways", {"claimCode": enroll["claimCode"], "name": "Hub ZAP"}, cookie=cookie), 201)

node = f"PLUG-{RUN}"
pairing = need("start a pairing", call("POST", "/v1/pairings", {"kind": "plug", "name": "Mesin ZAP", "roomId": room}, cookie=cookie), 201)["id"]
now = int(time.time())
need("send the first reading", call("POST", "/device/v1/readings", {
    "seq": 1, "sentAt": now, "nodes": [{"nodeId": node, "kind": "plug", "samples": [{"t": now - 2, "p": 100.0}]}],
}, bearer=key, app=False), 200)
paired = need("read the pairing", call("GET", "/v1/pairings/" + pairing, cookie=cookie), 200)

if paired.get("status") != "connected" or not paired.get("deviceId"):
    sys.exit(f"the plug {node} was not paired, {paired}")

device = paired["deviceId"]
today = time.strftime("%Y-%m-%d")
reading = {"seq": 2, "sentAt": now + 1, "nodes": [{"nodeId": node, "kind": "plug", "samples": [{"t": now, "p": 120.0}]}]}


def owner(method, path, body=None):
    item = {"method": method, "path": path, "cookie": cookie, "app": True}
    if body is not None:
        item["body"] = body

    return item


requests = [
    {"method": "GET", "path": "/healthz"},
    {"method": "GET", "path": "/readyz"},
    owner("GET", "/v1/auth/me"),
    owner("GET", "/v1/beranda"),
    owner("GET", "/v1/devices"),
    owner("GET", "/v1/devices/" + device),
    owner("GET", "/v1/recommendations"),
    owner("GET", "/v1/reports"),
    {"method": "POST", "path": "/device/v1/readings", "bearer": key, "body": reading},
    {"method": "POST", "path": "/v1/auth/login", "app": True, "body": {"email": EMAIL, "password": PASSWORD}},
    {"method": "POST", "path": "/v1/auth/login", "app": True, "body": {"email": EMAIL, "password": PASSWORD + "-wrong"}},
    {"method": "GET", "path": "/v1/beranda", "app": True},
    {"method": "GET", "path": "/v1/does-not-exist"},
]

if FULL:
    requests += [
        owner("GET", "/v1/settings"),
        owner("GET", "/v1/rooms"),
        owner("GET", "/v1/gateways"),
        owner("GET", "/v1/production"),
        owner("GET", f"/v1/devices/{device}/relay"),
        owner("POST", "/v1/rooms", {"name": "Ruang tambahan"}),
        owner("PUT", "/v1/production/" + today, {"quantity": 10, "unit": "potong"}),
        owner("PUT", f"/v1/devices/{device}/schedule", {"offAt": "17:30", "days": "mon-fri"}),
        owner("POST", f"/v1/devices/{device}/relay", {"state": "off"}),
        {"method": "GET", "path": "/device/v1/enroll/" + enroll["pollId"]},
        {"method": "POST", "path": "/v1/auth/forgot-password", "app": True, "body": {"email": EMAIL}},
    ]

with open(OUT, "w", encoding="utf-8") as handle:
    json.dump(requests, handle)

print(f"wrote {len(requests)} requests")
