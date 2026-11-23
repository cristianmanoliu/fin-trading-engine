"""Kraken Futures book screen: market-order impact vs mid at given USD notionals.
Method of crash_overlay_revival_prereg_2026-09-19.md §5 (bid side gates), median of N snapshots."""
import json, time, statistics, urllib.request
BOOK = "1000SHIB AAVE ARB BCH CRV HBAR INJ LTC NEAR SUI UNI WLD XLM".split()
EXCL = ("APT AXS CHZ DOT DYDX ENJ ENS ETC FIL GALA GMX GRT ICP IMX IOTA KAVA LDO MANA MKR OP "
        "PYTH ROSE RUNE SAND SEI SNX TIA VET ZIL 1INCH APE ATOM BLUR FTM").split()
SIZES = [28_000, 56_000]; SNAPS = 3; GATE = 15.0

def impact(levels, usd, mid):
    """Walk levels (best first); return bp of VWAP vs mid, or None if depth < usd."""
    got_usd = got_qty = 0.0
    for px, qty in levels:
        take = min(qty, (usd - got_usd) / px)
        got_usd += take * px; got_qty += take
        if got_usd >= usd - 1e-6:
            return abs(got_usd / got_qty - mid) / mid * 1e4
    return None

def screen(sym):
    k = "PF_SHIBUSD" if sym == "1000SHIB" else f"PF_{sym}USD"
    url = f"https://futures.kraken.com/derivatives/api/v3/orderbook?symbol={k}"
    d = json.load(urllib.request.urlopen(url, timeout=20))
    if d.get("result") != "success":
        return None
    bids = sorted(d["orderBook"]["bids"], key=lambda x: -x[0])
    asks = sorted(d["orderBook"]["asks"], key=lambda x: x[0])
    if not bids or not asks:
        return None
    mid = (bids[0][0] + asks[0][0]) / 2
    return {u: (impact(bids, u, mid), impact(asks, u, mid)) for u in SIZES}

if __name__ == "__main__":
    assert abs(impact([(100, 1000)], 28_000, 100.5) - 49.75) < 0.01  # one-level walk
    assert impact([(100, 1)], 28_000, 100) is None                  # thin book
    syms = BOOK + EXCL
    snaps = {s: [] for s in syms}
    for i in range(SNAPS):
        for s in syms:
            try:
                r = screen(s)
            except Exception:
                r = None
            if r: snaps[s].append(r)
        if i < SNAPS - 1: time.sleep(20)
    rows = []
    for s in syms:
        rs = snaps[s]
        if len(rs) < SNAPS:
            rows.append((s, None)); continue
        out = {}
        for u in SIZES:
            b = [r[u][0] for r in rs]; a = [r[u][1] for r in rs]
            out[u] = (None if None in b else statistics.median(b), None if None in a else statistics.median(a))
        rows.append((s, out))
    fmt = lambda x: "thin" if x is None else f"{x:5.1f}"
    print(f"{'sym':9} {'grp':5} {'bid28k':>7} {'bid56k':>7} {'ask56k':>7}  56k_pass")
    for s, o in rows:
        g = "book" if s in BOOK else "excl"
        if o is None:
            print(f"{s:9} {g:5}  no market"); continue
        b28, b56, a56 = o[28_000][0], o[56_000][0], o[56_000][1]
        ok = b56 is not None and b56 < GATE
        print(f"{s:9} {g:5} {fmt(b28):>7} {fmt(b56):>7} {fmt(a56):>7}  {'PASS' if ok else 'fail'}")
