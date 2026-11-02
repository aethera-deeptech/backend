"""Smoke test of the API on a throwaway stack, started by the Smoke stack workflow.

It signs a business up, pairs a gateway and a plug the way the app and the firmware do, sends a reading, and reads every screen of
the app once. It only uses the standard library, so the runner needs no packages.

Settings come from the environment
  BASE_URL          address of the API without a path, http://127.0.0.1:8080 by default. Plain http is only accepted for 127.0.0.1
  APP_ORIGIN        the origin that the API allows, http://localhost:5173 by default, which is the default of the service
  SMOKE_LOG_CMD     a command that prints the logs of the API. A stack with no mail server writes the verification link there
"""

import hashlib
import json
import os
import re
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
RUN = time.strftime("%m%d%H%M%S")
EMAIL = f"smoke-{RUN}@example.com"
PASSWORD = "Smoke-" + secrets.token_urlsafe(18) + "1!"

# The address is parsed and not matched by its start, so a name such as http://127.0.0.1@other.example cannot pass
URL = urlsplit(BASE)
LOCAL = URL.scheme == "http" and URL.hostname == "127.0.0.1" and URL.username is None and not URL.path.strip("/")
SECURE = URL.scheme == "https" and bool(URL.hostname) and URL.username is None and not URL.path.strip("/")

if not (LOCAL or SECURE) or not LOG_CMD:
    sys.exit("set BASE_URL (https, or http for 127.0.0.1 only) and SMOKE_LOG_CMD")

passed = 0
failed = []


class Answer:
    """The status, the headers and the body of one answer"""

    def __init__(self, status, headers, raw):
        self.status = status
        self.headers = headers
        self.raw = raw

    def json(self):
        try:
            return json.loads(self.raw)
        except ValueError:
            return {}


def call(method, path, body=None, cookie="", bearer="", app=True):
    """Sends one request. An app request carries the CSRF header and the origin, a gateway request carries neither."""
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
        with urllib.request.urlopen(req, timeout=20) as res:  # noqa: S310 - the address is checked above
            return Answer(res.status, res.headers, res.read())
    except urllib.error.HTTPError as err:
        return Answer(err.code, err.headers, err.read())


def check(name, ok, detail=""):
    global passed
    if ok:
        passed += 1
        print(f"ok   {name}")
    else:
        failed.append(name)
        print(f"FAIL {name} {detail}")


def expect(name, answer, status):
    check(name, answer.status == status, f"answered {answer.status} {answer.raw[:200]!r}")

    return answer.json() if answer.status == status else {}


def link_for(email):
    """Reads the verification link of an email from the logs of the API, where a stack with no mail server writes it"""
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

            if entry.get("to") == email:
                return entry.get("link", "")

        time.sleep(1)

    return ""


def session_cookie(answer):
    """The name and the value of the first cookie that an answer sets"""
    header = answer.headers.get("Set-Cookie", "")

    return header.split(";", 1)[0] if "=" in header else ""


def main():
    health = call("GET", "/healthz", app=False)
    body = expect("healthz answers", health, 200)
    check("healthz reports a version", body.get("status") == "ok" and bool(body.get("version")), health.raw[:200])
    expect("readyz answers", call("GET", "/readyz", app=False), 200)

    expect("a screen needs a session", call("GET", "/v1/devices"), 401)
    expect("a request that changes data needs the app header", call("POST", "/v1/auth/login", {"email": EMAIL, "password": PASSWORD}, app=False), 403)

    expect("sign up", call("POST", "/v1/auth/register", {"businessName": "Toko Smoke", "email": EMAIL, "password": PASSWORD}), 201)
    link = link_for(EMAIL)
    token = (parse_qs(urlsplit(link).query).get("token") or [""])[0]
    check("the verification link reached the log", bool(token), link)

    verified = call("POST", "/v1/auth/verify-email", {"token": token})
    expect("verify the email", verified, 200)
    cookie = session_cookie(verified)
    check("verifying signs in", bool(cookie))

    me = expect("who am I", call("GET", "/v1/auth/me", cookie=cookie), 200)
    check("the account is mine", me.get("email") == EMAIL, str(me)[:200])
    expect("sign in with the password", call("POST", "/v1/auth/login", {"email": EMAIL, "password": PASSWORD}), 200)

    expect("settings", call("GET", "/v1/settings", cookie=cookie), 200)
    room = expect("make a room", call("POST", "/v1/rooms", {"name": "Ruang jahit"}, cookie=cookie), 201).get("id", "")
    check("the room has an id", bool(room))

    # the gateway makes its own key and sends only the hash of it
    key = secrets.token_urlsafe(32)
    enroll = expect("the gateway asks for a code", call("POST", "/device/v1/enroll", {
        "hardwareId": f"SMOKE-{RUN}", "firmware": "smoke", "keyHash": hashlib.sha256(key.encode()).hexdigest(),
    }, app=False), 201)
    expect("the code is not claimed yet", call("GET", "/device/v1/enroll/" + enroll.get("pollId", "x" * 32), app=False), 202)

    gateway = expect("the owner claims the code", call("POST", "/v1/gateways", {"claimCode": enroll.get("claimCode", ""), "name": "Hub"}, cookie=cookie), 201)
    check("the gateway has an id", bool(gateway.get("id")))
    claimed = expect("the gateway learns that it is claimed", call("GET", "/device/v1/enroll/" + enroll.get("pollId", "x" * 32), app=False), 200)
    check("the poll names the gateway", claimed.get("gatewayId") == gateway.get("id"), str(claimed))

    pairing = expect("start a pairing", call("POST", "/v1/pairings", {"kind": "plug", "name": "Mesin jahit 1", "roomId": room}, cookie=cookie), 201).get("id", "")
    now = int(time.time())
    sent = call("POST", "/device/v1/readings", {
        "seq": 1, "sentAt": now, "nodes": [{"nodeId": "PLUG-0001", "kind": "plug", "samples": [{"t": now - 5, "p": 120.5}]}],
    }, bearer=key, app=False)
    ack = expect("the gateway sends a reading", sent, 200)
    check("the sample is stored", ack.get("accepted") == 1, str(ack))
    expect("a wrong key is refused", call("POST", "/device/v1/readings", {"seq": 2, "sentAt": now, "nodes": []}, bearer="x" * 43, app=False), 401)

    status = expect("read the pairing", call("GET", "/v1/pairings/" + pairing, cookie=cookie), 200)
    device = status.get("deviceId") or ""
    check("the plug is paired", status.get("status") == "connected" and bool(device), str(status))

    listed = expect("list the devices", call("GET", "/v1/devices", cookie=cookie), 200)
    check("the list holds the plug", len(listed.get("items", [])) == 1, str(listed)[:200])
    detail = expect("read the device", call("GET", "/v1/devices/" + device, cookie=cookie), 200)
    check("the plug is working or waiting", detail.get("status") in ("nyala", "siaga", "mati"), str(detail.get("status")))

    expect("the home screen", call("GET", "/v1/beranda", cookie=cookie), 200)
    tips = expect("the tips", call("GET", "/v1/recommendations", cookie=cookie), 200)
    check("a new business is still collecting", tips.get("phase") == "collecting", str(tips)[:200])
    report = expect("the report", call("GET", "/v1/reports", cookie=cookie), 200)
    pdf = call("GET", "/v1/reports/" + str(report.get("period", "")) + "/pdf", cookie=cookie)
    check("the report as a PDF", pdf.status == 200 and pdf.raw.startswith(b"%PDF") and "pdf" in pdf.headers.get("Content-Type", ""), f"answered {pdf.status}")

    expect("sign out", call("POST", "/v1/auth/logout", cookie=cookie), 204)
    expect("the session is gone", call("GET", "/v1/auth/me", cookie=cookie), 401)


try:
    main()
except Exception as err:  # noqa: BLE001 - a crash is reported as a failure of the run
    failed.append(f"crash {type(err).__name__} {err}")

print(f"\n{passed} passed, {len(failed)} failed")

if failed:
    print("failed " + ", ".join(failed))
    sys.exit(1)
