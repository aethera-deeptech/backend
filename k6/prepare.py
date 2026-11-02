"""Makes the data that the k6 scripts need on a throwaway stack, started by the Performance workflow before k6.

It signs a business up, reads the verification link from the log of the API, makes a room, enrolls and claims a few gateways, and pairs
a few plugs on each one the way the app and the firmware do. It writes k6/setup.json with the session of the owner and the key, the next
sequence number and the nodes of every gateway. The file holds secrets of a throwaway stack, so it is not committed and is not uploaded.
It only uses the standard library.

Settings come from the environment
  BASE_URL      address of the API without a path, http://127.0.0.1:8080 by default, plain http for 127.0.0.1 only
  APP_ORIGIN    the origin that the API allows, http://localhost:5173 by default
  SMOKE_LOG_CMD a command that prints the logs of the API, where a stack with no mail server writes the verification link
  GATEWAYS      how many gateways to make, 6 by default. The claim limit of one business is 10 in an hour
  PLUGS         how many plugs on each gateway, 4 by default. The pairing limit of one business is 30 in an hour
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
GATEWAYS = int(os.environ.get("GATEWAYS", "6"))
PLUGS = int(os.environ.get("PLUGS", "4"))
RUN = time.strftime("%m%d%H%M%S")
EMAIL = f"perf-{RUN}@example.com"
PASSWORD = "Perf-" + secrets.token_urlsafe(18) + "1!"

URL = urlsplit(BASE)
LOCAL = URL.scheme == "http" and URL.hostname == "127.0.0.1" and URL.username is None and not URL.path.strip("/")

if not LOCAL or not LOG_CMD or GATEWAYS * PLUGS > 30 or GATEWAYS > 10:
    sys.exit("set BASE_URL to http://127.0.0.1, set SMOKE_LOG_CMD, and keep GATEWAYS at 10 or less and GATEWAYS times PLUGS at 30 or less")


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
    """Stops the run when a step of the set up does not answer as expected, because k6 would only fail later and with less to read"""
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


need("sign up", call("POST", "/v1/auth/register", {"businessName": "Toko Perf", "email": EMAIL, "password": PASSWORD}), 201)
token = (parse_qs(urlsplit(verification_link()).query).get("token") or [""])[0]
if not token:
    sys.exit("the verification link did not reach the log")

status, headers, _ = call("POST", "/v1/auth/verify-email", {"token": token})
cookie = headers.get("Set-Cookie", "").split(";", 1)[0]
if status != 200 or "=" not in cookie:
    sys.exit(f"verify the email answered {status}")

room = need("make a room", call("POST", "/v1/rooms", {"name": "Ruang produksi"}, cookie=cookie), 201)["id"]

gateways = []
devices = []

for g in range(GATEWAYS):
    key = secrets.token_urlsafe(32)
    enroll = need("enroll", call("POST", "/device/v1/enroll", {
        "hardwareId": f"PERF-{RUN}-{g}", "firmware": "perf", "keyHash": hashlib.sha256(key.encode()).hexdigest(),
    }, app=False), 201)
    need("claim", call("POST", "/v1/gateways", {"claimCode": enroll["claimCode"], "name": f"Hub {g + 1}"}, cookie=cookie), 201)

    seq = 0
    nodes = []

    for p in range(PLUGS):
        node = f"PLUG-{RUN}-{g}-{p}"
        pairing = need("start a pairing", call("POST", "/v1/pairings", {"kind": "plug", "name": f"Mesin {g + 1}-{p + 1}", "roomId": room}, cookie=cookie), 201)["id"]
        seq += 1
        now = int(time.time())
        need("send the first reading", call("POST", "/device/v1/readings", {
            "seq": seq, "sentAt": now, "nodes": [{"nodeId": node, "kind": "plug", "samples": [{"t": now - 2, "p": 100.0}]}],
        }, bearer=key, app=False), 200)
        paired = need("read the pairing", call("GET", "/v1/pairings/" + pairing, cookie=cookie), 200)

        if paired.get("status") != "connected" or not paired.get("deviceId"):
            sys.exit(f"the plug {node} was not paired, {paired}")

        nodes.append(node)
        devices.append(paired["deviceId"])

    gateways.append({"key": key, "seq": seq, "nodes": nodes})

with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "setup.json"), "w", encoding="utf-8") as handle:
    json.dump({"cookie": cookie, "devices": devices, "gateways": gateways}, handle)

print(f"made {len(gateways)} gateways, {len(devices)} devices")
