"""Smoke test of the deployed API, started by hand from the Smoke workflow after a deploy.

It only reads. It signs in as the keeper account, opens every screen of the app once, and signs out. It creates nothing and changes
nothing, so it is safe to run against production. The keeper account is made once by hand and verified by email.

Settings come from the environment
  BASE_URL               address of the API without a path, https only
  APP_ORIGIN             the origin of the app, which the API allows
  EXPECTED_VERSION       the version that /healthz has to report
  SMOKE_KEEPER_EMAIL     the keeper account
  SMOKE_KEEPER_PASSWORD  its password
"""

import json
import os
import re
import sys
import urllib.error
import urllib.request
from urllib.parse import urlsplit

BASE = os.environ.get("BASE_URL", "").rstrip("/")
ORIGIN = os.environ.get("APP_ORIGIN", "")
EXPECTED = os.environ.get("EXPECTED_VERSION", "")
EMAIL = os.environ.get("SMOKE_KEEPER_EMAIL", "")
PASSWORD = os.environ.get("SMOKE_KEEPER_PASSWORD", "")

# The address is parsed and not matched by its start, so a name such as https://api.example@other.example cannot pass
URL = urlsplit(BASE)
SECURE = URL.scheme == "https" and bool(URL.hostname) and URL.username is None and not URL.path.strip("/") and not URL.query

if not SECURE or not ORIGIN or not EXPECTED or not EMAIL or not PASSWORD:
    sys.exit("set BASE_URL (https), APP_ORIGIN, EXPECTED_VERSION, SMOKE_KEEPER_EMAIL and SMOKE_KEEPER_PASSWORD")

passed = 0
failed = []


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Answers a redirect as it is, so a check of a status never passes on the page that a redirect leads to"""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


OPENER = urllib.request.build_opener(NoRedirect)


class Answer:
    def __init__(self, status, headers, raw):
        self.status = status
        self.headers = headers
        self.raw = raw

    def json(self):
        try:
            return json.loads(self.raw)
        except ValueError:
            return {}


def call(method, path, body=None, cookie=""):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Requested-With", "aethera")
    req.add_header("Origin", ORIGIN)

    if cookie:
        req.add_header("Cookie", cookie)

    try:
        with OPENER.open(req, timeout=20) as res:
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
    check(name, answer.status == status, f"answered {answer.status}")

    return answer.json() if answer.status == status else {}


def main():
    health = call("GET", "/healthz")
    body = expect("healthz answers", health, 200)
    check("healthz reports the expected version", body.get("version") == EXPECTED, f"reported {body.get('version')}")
    expect("readyz answers", call("GET", "/readyz"), 200)

    expect("a screen needs a session", call("GET", "/v1/devices"), 401)

    login = call("POST", "/v1/auth/login", {"email": EMAIL, "password": PASSWORD})
    expect("sign in as the keeper", login, 200)
    header = login.headers.get("Set-Cookie", "")
    cookie = header.split(";", 1)[0] if "=" in header else ""
    check("signing in sets a session", cookie.startswith("__Host-aethera_session="))

    me = expect("who am I", call("GET", "/v1/auth/me", cookie=cookie), 200)
    check("the account is the keeper", me.get("email") == EMAIL, "another account answered")

    for name, path in [
        ("settings", "/v1/settings"),
        ("rooms", "/v1/rooms"),
        ("gateways", "/v1/gateways"),
        ("devices", "/v1/devices"),
        ("the home screen", "/v1/beranda"),
        ("the tips", "/v1/recommendations"),
        ("the production list", "/v1/production"),
    ]:
        expect(name, call("GET", path, cookie=cookie), 200)

    report = expect("the report", call("GET", "/v1/reports", cookie=cookie), 200)
    period = str(report.get("period", ""))
    check("the report has a period", re.fullmatch(r"[0-9]{4}-(0[1-9]|1[0-2])", period) is not None, period)

    expect("sign out", call("POST", "/v1/auth/logout", cookie=cookie), 204)
    expect("the session is gone", call("GET", "/v1/auth/me", cookie=cookie), 401)


try:
    main()
except Exception as err:
    failed.append(f"crash {type(err).__name__}")

print(f"\n{passed} passed, {len(failed)} failed")

if failed:
    print("failed " + ", ".join(failed))
    sys.exit(1)
