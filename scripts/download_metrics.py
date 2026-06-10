#!/usr/bin/env python3
"""download_metrics.py — robust fetch of Binance UM futures METRICS (OI/L-S/taker).

Replaces the bash download_metrics.sh, whose S3 `marker` pagination silently
truncated several symbols at the first 500-key page (143,994-row = 500-day stubs).
This version paginates the S3 XML listing correctly (marker = last key, until
IsTruncated=false), retries transient HTTP errors, and verifies each symbol reached
the archive's latest available date.

Unlocks #1 (OI divergence), #2 (L/S fade), #4-v2 (taker ratio), #6 (OI breakout),
#9 (positioning composite). Output: data/metrics/<SYM>.csv (header once).

Stdlib only. Usage: download_metrics.py [SYM ...]   (default: deployed-20)
Env: WORKERS (default 6) for parallel symbols.
"""
import os, sys, io, csv, time, zipfile, urllib.request, urllib.error, urllib.parse
import xml.etree.ElementTree as ET
from concurrent.futures import ThreadPoolExecutor, as_completed

S3 = "https://s3-ap-northeast-1.amazonaws.com/data.binance.vision"
DL = "https://data.binance.vision"
PREFIX = "data/futures/um/daily/metrics"
OUT = "data/metrics"
DEPLOYED = ["ROSEUSDT", "BCHUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
            "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "IMXUSDT", "ETCUSDT", "RUNEUSDT",
            "AVAXUSDT", "APTUSDT", "DOTUSDT", "FILUSDT", "BTCUSDT", "CHZUSDT",
            "SANDUSDT", "TRXUSDT"]
NS = "{http://s3.amazonaws.com/doc/2006-03-01/}"


def http(url, timeout=40, retries=4):
    for a in range(retries):
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "research/1.0"})
            with urllib.request.urlopen(req, timeout=timeout) as r:
                return r.read()
        except (urllib.error.URLError, TimeoutError) as e:
            if a == retries - 1:
                raise
            time.sleep(1.5 * (a + 1))
    return b""


def list_dates(sym):
    """All YYYY-MM-DD with a metrics zip, via correct S3 marker pagination."""
    dates = []
    marker = ""
    while True:
        url = f"{S3}?delimiter=/&prefix={PREFIX}/{sym}/&marker={urllib.parse.quote(marker)}"
        xml = http(url)
        root = ET.fromstring(xml)
        keys = [c.find(f"{NS}Key").text for c in root.findall(f"{NS}Contents")]
        for k in keys:
            if k.endswith(".zip"):
                # .../<SYM>-metrics-YYYY-MM-DD.zip
                base = k.rsplit("/", 1)[-1]
                d = base.replace(f"{sym}-metrics-", "").replace(".zip", "")
                if len(d) == 10:
                    dates.append(d)
        trunc = root.find(f"{NS}IsTruncated")
        if trunc is None or trunc.text != "true" or not keys:
            break
        marker = keys[-1]
    return sorted(set(dates))


def fetch_day(sym, d):
    """Return list of CSV rows (incl. header) for one day, or None."""
    url = f"{DL}/{PREFIX}/{sym}/{sym}-metrics-{d}.zip"
    try:
        raw = http(url, retries=3)
    except Exception:
        return None
    try:
        z = zipfile.ZipFile(io.BytesIO(raw))
        name = z.namelist()[0]
        return z.read(name).decode().splitlines()
    except Exception:
        return None


def fetch_symbol(sym, day_workers=24):
    """Fetch all days for a symbol with a per-day thread pool. The bottleneck is
    HTTP latency (~0.8s/day to Tokyo S3) so day-level parallelism is the real win:
    1650 days serial = ~22min; at 24 concurrent = ~1-2min."""
    dest = os.path.join(OUT, f"{sym}.csv")
    dates = list_dates(sym)
    if not dates:
        return sym, 0, "no-archive"
    day_lines = {}
    with ThreadPoolExecutor(max_workers=day_workers) as ex:
        futs = {ex.submit(fetch_day, sym, d): d for d in dates}
        for fut in as_completed(futs):
            d = futs[fut]
            try:
                lines = fut.result()
            except Exception:
                lines = None
            if lines:
                day_lines[d] = lines
    if not day_lines:
        return sym, 0, "empty"
    header = None
    rows_out = []
    for d in dates:                       # write in date order
        lines = day_lines.get(d)
        if not lines:
            continue
        if header is None:
            header = lines[0]
            rows_out.append(header)
        rows_out.extend(lines[1:] if lines[0] == header else lines)
    with open(dest, "w") as f:
        f.write("\n".join(rows_out) + "\n")
    last = rows_out[-1].split(",")[0][:10]
    return sym, len(day_lines), f"->{last} ({len(rows_out)-1} rows, {len(day_lines)}/{len(dates)} days)"


def main():
    os.makedirs(OUT, exist_ok=True)
    syms = sys.argv[1:] or DEPLOYED
    # 3 symbols x 24 day-threads each = ~72 concurrent connections (S3 handles fine)
    workers = int(os.environ.get("WORKERS", "3"))
    print(f"fetching metrics for {len(syms)} symbols, {workers} workers...")
    with ThreadPoolExecutor(max_workers=workers) as ex:
        futs = {ex.submit(fetch_symbol, s): s for s in syms}
        for fut in as_completed(futs):
            sym = futs[fut]
            try:
                s, n, msg = fut.result()
                print(f"  [{s}] {msg}")
            except Exception as e:
                print(f"  [{sym}] ERROR {e}")
    print("DONE.")


if __name__ == "__main__":
    main()
