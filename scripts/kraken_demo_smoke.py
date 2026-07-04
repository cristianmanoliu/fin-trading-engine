#!/usr/bin/env python3
"""Kraken Futures DEMO auth smoke test (venue_port_admin_checklist.md step 7).

Read-only: authenticated GET /derivatives/api/v3/accounts must return 200.
No orders, no adapter code — admin verification only.

Signing scheme (Kraken Futures API v3):
  Authent = b64( HMAC-SHA512( b64decode(secret),
                              SHA256(postData + nonce + endpointPath) ) )
  where endpointPath strips the leading "/derivatives".
"""
import base64
import hashlib
import hmac
import json
import os
import sys
import time
import urllib.request

BASE = "https://demo-futures.kraken.com"


def load_env(path):
    creds = {}
    with open(os.path.expanduser(path)) as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                k, v = line.split("=", 1)
                creds[k] = v
    return creds["KRAKEN_DEMO_API_KEY"], creds["KRAKEN_DEMO_API_SECRET"]


def sign(secret, post_data, nonce, endpoint_path):
    message = (post_data + nonce + endpoint_path).encode()
    sha = hashlib.sha256(message).digest()
    mac = hmac.new(base64.b64decode(secret), sha, hashlib.sha512)
    return base64.b64encode(mac.digest()).decode()


def get(api_key, secret, path):
    signing_path = path.removeprefix("/derivatives")
    nonce = str(int(time.time() * 1000))
    req = urllib.request.Request(BASE + path, headers={
        "APIKey": api_key,
        "Nonce": nonce,
        "Authent": sign(secret, "", nonce, signing_path),
    })
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read()
            status = resp.status
    except urllib.error.HTTPError as e:
        raw = e.read()
        status = e.code
    try:
        return status, json.loads(raw)
    except json.JSONDecodeError:
        return status, {"raw": raw[:400].decode(errors="replace")}


def main():
    api_key, secret = load_env("~/.kraken-futures-demo.env")
    status, body = get(api_key, secret, "/derivatives/api/v3/accounts")
    result = body.get("result", "?")
    print(f"GET /derivatives/api/v3/accounts -> HTTP {status}, result={result}")
    if status == 200 and result == "success":
        accounts = body.get("accounts", {})
        for name, acct in list(accounts.items())[:5]:
            bal = acct.get("balances", acct.get("auxiliary", {}))
            print(f"  account {name}: type={acct.get('type', '?')} balances={bal}")
        print("SMOKE PASS")
        return 0
    print(f"  body: {json.dumps(body)[:400]}")
    print("SMOKE FAIL")
    return 1


if __name__ == "__main__":
    sys.exit(main())
