# Venue-port admin checklist — Kraken account prep (2026-07-04)

**Scope:** operator-side ADMINISTRATIVE prep only — accounts, KYC, API keys, funding
rails. **No executor code, no port work, no real trades** — those stay locked behind
the forward-paper verdict per `results/venue_scouting_2026-06-10.md` and the port lock
in CLAUDE.md. The scouting note's "no accounts until verdict" non-action is overridden
by explicit operator direction (2026-07-04): KYC latency is dead time post-verdict and
account prep costs nothing under a KILL verdict.

**Why now:** verdict lands ~2026-08-25 (150-trade gate). Kraken KYC + futures
activation + SEPA rails can take days-to-weeks; doing it in parallel shaves that off
time-to-STAGE_1 if the verdict is PROMOTE (or OPERATOR_REVIEW resolved as promote).

---

## Verified live 2026-07-04 (public instruments API, no auth)

All 16 deployed symbols tradeable on Kraken Futures (`futures.kraken.com/derivatives/api/v3/instruments`):

| Binance | Kraken perp | tier-1 max leverage | tick size |
|---|---|---:|---|
| ROSEUSDT | PF_ROSEUSD | 10× | 1e-06 |
| BCHUSDT | PF_BCHUSD | 50× | 0.01 |
| GRTUSDT | PF_GRTUSD | 20× | 1e-05 |
| 1INCHUSDT | PF_1INCHUSD | 10× | 1e-05 |
| ADAUSDT | PF_ADAUSD | 50× | 1e-05 |
| KAVAUSDT | PF_KAVAUSD | 10× | 1e-05 |
| 1000SHIBUSDT | PF_SHIBUSD | 50× | 1e-09 |
| ENSUSDT | PF_ENSUSD | 20× | 0.001 |
| XLMUSDT | PF_XLMUSD | 50× | 1e-05 |
| IMXUSDT | PF_IMXUSD | 20× | 0.0001 |
| ETCUSDT | PF_ETCUSD | 50× | 0.001 |
| RUNEUSDT | PF_RUNEUSD | 20× | 0.0001 |
| AVAXUSDT | PF_AVAXUSD | 50× | 0.001 |
| APTUSDT | PF_APTUSD | 50× | 0.0001 |
| DOTUSDT | PF_DOTUSD | 50× | 0.0001 |
| FILUSDT | PF_FILUSD | 50× | 0.0001 |

Notes vs the 2026-06-10 scouting doc:

- **Leverage is better than the scouted blanket 10×:** 50× on 8 symbols, 20× on 5,
  10× only on ROSE/1INCH/KAVA. Tier-1 initial margin = 2% / 5% / 10% respectively.
  CAVEAT: instrument max ≠ what an EEA retail client is granted — verify actual
  post-KYC limits (MiFID appropriateness outcome may cap lower). STAGE_1 margin
  estimate stays conservatively at ~$1.5–2k until verified.
- **1000SHIB unit conversion:** Binance trades the 1000-SHIB contract; Kraken trades
  raw SHIB (PF_SHIBUSD, tick 1e-9). The port executor must convert qty ×1000 and
  price ÷1000 for this symbol. Port-time detail; recorded here so it isn't lost.
- All 16 are `flexible_futures` (multi-collateral perps), funding coefficient 8h —
  funding-model re-basing vs our Binance 8h-cadence CSVs is a port-time task
  (scouting finding #5), not admin.

---

## Checklist — Kraken (presumptive primary)

Do in order; each step gates the next.

- [ ] **1. Create Kraken account** at kraken.com — Romania onboards under
  **Payward Europe (CySEC 342/17, MiFID II)**. Use a dedicated email; enable
  2FA (authenticator app, not SMS) immediately.
- [ ] **2. KYC to Intermediate, then Pro** — ID document + proof of residence +
  occupation/funding questionnaire. Pro is the tier futures wants; Intermediate
  unlocks SEPA. Expect hours-to-days.
- [ ] **3. Activate Kraken Futures** — sign in at futures.kraken.com with the same
  account; complete the EEA derivatives **appropriateness questionnaire** (MiFID II).
  Record the leverage limits actually granted (see caveat above).

  **Status 2026-07-07: FAILED the appropriateness questionnaire** — "not
  currently eligible", retake allowed after 30 days (**~2026-08-06**, before
  the ~2026-08-25 verdict, so the timeline still works with one clean pass).
  Consequences while blocked: no futures activation → no futures API keys →
  prod smoke (step 7 pivot) blocked too; steps 4/6/8 gate on this. Before the
  retake: review derivatives mechanics (margin, leverage, liquidation,
  funding, max-loss scenarios) — answer honestly, do NOT inflate real-money
  trading history. If the retake also fails, re-rank venues: OKX EU likely
  has the same MiFID gate; Hyperliquid has no appropriateness gate (DEX,
  different risk profile — needs its own scouting pass). Support cannot
  override MiFID eligibility; don't burn time there.
- [ ] **4. Verify the fee schedule on YOUR account** — expect base tier
  5bp taker / 2bp maker (10bp RT = the exact `--fee-bps 10` backtest assumption).
  Screenshot/record the EEA fee page for the port pre-reg.
- [ ] **5. SEPA EUR test deposit** (~€100) — verifies the funding rail end-to-end.
  Do NOT fund beyond a test amount before the verdict. EUR is accepted as futures
  collateral (scouting finding #2) — confirm EUR shows as usable margin collateral
  in the futures wallet, or whether conversion to USD-equivalent is required.

  **Status 2026-07-07: rail VERIFIED** — €10 SEPA deposit landed (shows as
  ~10.72 USD in the spot portfolio view; whether that is display-currency
  or an actual conversion is unresolved). The futures-collateral half of
  this step is blocked on step 3 (no futures wallet access until the
  appropriateness retake passes). Deposit sits idle per the lock.
- [ ] **6. Create TWO API key pairs** on futures.kraken.com (Settings → API keys):
  - `read-only` — monitoring/reconciliation.
  - `trade` — order placement (Kraken Futures keys cannot withdraw; withdrawals
    live on the spot account — confirm this on the key-creation screen).
  Store in `~/.kraken-futures.env` (same pattern as `~/.binance-testnet.env`);
  never in the repo. Note creation date for rotation hygiene.
- [x] **7. Demo environment** — create a **demo-futures.kraken.com** account +
  demo API keys. This is the Layer-2/Layer-3-equivalent venue for the port's
  validation gates; confirming it works NOW de-risks the port timeline.
  Smoke test: authenticated `GET /derivatives/api/v3/accounts` returns 200.

  **Status 2026-07-04:** demo account + full-access API keys created (operator).
  Creds in `~/.kraken-futures-demo.env` (chmod 600, NOT in repo). Smoke tool
  committed: `scripts/kraken_demo_smoke.py` (read-only, stdlib-only; implements
  the Futures v3 signing scheme — HMAC-SHA512 over SHA256(postData+nonce+path),
  path stripped of `/derivatives`). **200-confirmation PENDING:** the demo
  `/derivatives` REST gateway returned 503 for ALL endpoints incl. public
  unauthenticated ones (instruments/tickers) while prod returned 200 and the
  demo history API worked — a demo-side partial outage, not an auth problem.
  Re-run `python3 scripts/kraken_demo_smoke.py` until it prints SMOKE PASS.
  **Port-pre-reg note:** demo REST availability is NOT 100%; the port's
  Layer-2/3-equivalent gates must tolerate demo outages (retry/backoff, don't
  count outage windows as parity failures).

  **Status 2026-07-06:** outage ONGOING (day 3). Ruled out on our end:
  VPS (different IP) gets the identical 503-demo/200-prod split, so it is
  NOT an IP block (cf. the Layer-3 reconciler self-ban); Kraken's status
  page does not cover the demo environment, so there is no incident feed.
  **Retry policy (operator-chosen): MANUAL — run
  `python3 scripts/kraken_demo_smoke.py` at the START of every session
  until SMOKE PASS.** An automated watcher exists but is deliberately NOT
  installed: `scripts/kraken_demo_recovery_watch.sh` (tested; polls demo
  public endpoint, one-shot plain-text Telegram on 200, marker-file
  disarm). If the outage drags past ~2026-07-13, reconsider installing it
  (one crontab line on the VPS) and/or contact Kraken support.

  **Status 2026-07-06 (later, outage day 4): PIVOT to prod smoke —
  operator decision.** The step-7 INTENT (confirm our v3 signing code
  against a live Kraken Futures gateway) no longer waits on the demo:
  `scripts/kraken_demo_smoke.py --prod` smokes the REAL
  futures.kraken.com account instead (read-only `GET /accounts`, no
  orders, no balance needed; creds from step 6's read-only key in
  `~/.kraken-futures.env`, keys `KRAKEN_API_KEY`/`KRAKEN_API_SECRET`).
  Tests: `scripts/test_kraken_demo_smoke.py`. Prod smoke is gated on the
  operator completing steps 1–6 (started 2026-07-06). Demo remains the
  preferred order-placement sandbox at port time; if the demo env is
  still unreliable then, the port pre-reg may designate €100-scale prod
  micro-orders as the Layer-2-equivalent — that is a PORT-TIME pre-reg
  decision, NOT sanctioned now. The manual demo retry continues only as
  a low-effort session-start habit until either SMOKE PASS or prod
  smoke passes (whichever first closes step 7's intent).
- [ ] **8. Record everything** in a short note (account tier granted, leverage
  limits, fee tier, collateral behavior, demo creds location) — input to the
  port pre-registration.

## Checklist — OKX EU (fallback, optional / lower priority)

- [ ] Create account under the OKX EU (MiCA/MiFID II) entity; KYC only.
- [ ] Verify whether **crypto-perp retail access** is actually granted to a Romanian
  retail client (scouting couldn't confirm; their API geo-403'd our IP). If perps
  aren't retail-accessible, note it and stop — Bybit EU re-check happens at port time.

## What NOT to do (unchanged locks)

- No executor code, no Kraken adapter, no port pre-registration work (that opens at
  verdict; needs operator sanction to draft earlier).
- No real positions anywhere; test deposit stays idle. **The 2026-07-06 prod-smoke
  pivot does NOT relax this: prod API access is read-only verification, the €100
  step-5 deposit sits untouched, and no order is placed on any venue before the
  forward-paper verdict.**
- No changes to the live paper run, its symbols, or its config.
