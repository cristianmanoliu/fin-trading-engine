# Security note — leaked Telegram bot token in git history

**Status: OPEN, accepted risk. Not remediated. Recorded 2026-08-06.**

This file deliberately contains **no secret values**. It records where a secret
is, so a future reader does not have to rediscover it.

## Finding

`gitleaks detect` over full history (590 commits) reports one leak:

| | |
|---|---|
| Rule | `telegram-bot-api-token` |
| File | `deploy/install.sh`, line 22 |
| Commit | `0dfc9c57d55c52a9587c651c025e2abd3fb5d120` ("Initial snapshot") |
| Date | 2026-05-06 |
| Entropy | 4.97 |

A Telegram bot token was hardcoded in the initial snapshot commit.

## What is and isn't affected

- **The current tree is clean.** `deploy/install.sh` now reads the token from
  the environment (`TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}`); the hardcoded
  value was removed in `a680db6`. Nothing in `HEAD` contains it.
- **The value is still reachable in history**, so anyone who can clone this
  repo can recover it (`git show 0dfc9c5:deploy/install.sh`).
- **The repository is PRIVATE** on GitHub
  (`cristianmanoliu/fin-trading-engine`), so exposure is bounded by who has
  repo access — this is why the risk was accepted rather than treated as an
  emergency.
- **The token was still live when checked** on 2026-08-06, verified with a
  read-only `getMe` call (no message sent). It has **not** been revoked.

## Blast radius — this is a SHARED bot

The leaked token does **not** belong to this project's dedicated bot. It
belongs to a general-purpose bot that also appears in, at
minimum:

- `fin-reddit-mention-spike`
- `fin-wsb-dd-monitor`
- `fin-insider-edge`
- `aiscore-scraper`

**Consequence: revoking it breaks every one of those projects at once.** Any
rotation has to be coordinated across all of them, not done here in isolation.
That is the main reason this is a deferred decision rather than a quick fix.

> Note for future sessions: an earlier memory recorded this project as using a
> dedicated bot `@CCC_fin_trading_engine_bot`. That is **not** the bot whose
> token leaked. Do not assume the blast radius is limited to this repo.

## What to do when it is addressed

Rotation is the only fix that actually helps. History rewriting alone does not
— it removes the copy while leaving the credential valid.

1. **Revoke + reissue** in BotFather (`/revoke`). This invalidates the leaked
   value immediately and is the step that matters.
2. **Update every consumer** listed above with the new token. On a live VPS
   that meant `/etc/paper-live/env` only — config, no redeploy. That host no
   longer exists for this project.
3. **Optionally** scrub history with `git-filter-repo` and force-push. This
   rewrites all 590 commits and changes every SHA. Only worth doing if the
   repo might ever go public; **it is not a substitute for step 1.**

Re-verify with `gitleaks detect --no-banner --redact -v` (the pre-commit hook
only scans *new* commits, which is why a historical leak stayed invisible for
three months).

## Why this is filed and not fixed

This project is closed (see the banner in `CLAUDE.md`). Its own Telegram path
is dead: the VPS was destroyed 2026-08-04 and `/etc/paper-live/env` shredded,
so nothing here sends messages. The operator was asked on 2026-08-06 and chose
to document rather than rotate, consistent with the earlier decision to defer
rotation. The residual risk is a live credential recoverable by anyone with
access to this private repo, affecting the four other projects above.
