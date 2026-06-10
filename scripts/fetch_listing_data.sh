#!/usr/bin/env bash
# fetch_listing_data.sh — de-survivorship fetch for candidate #12 (new-listing drift).
#
# Downloads the FIRST ~2 months of daily (1d) klines + the full fundingRate history
# for EVERY USDT perp ever listed on Binance UM futures — including DELISTED symbols
# (the whole point: our local data/ has only 57 survivors, which biases the listing
# drift test because the worst listings delisted). Source: data.binance.vision.
#
# Output:
#   data/listing/klines/<SYM>-1d.csv     (concatenated first ~2mo of daily candles)
#   data/listing/funding/<SYM>.csv       (funding_time_ms,funding_rate — first months)
#
# Idempotent: skips a symbol whose kline CSV already exists. Tiny payloads (~1.5KB/zip).
# Pure curl + unzip; no auth. Run from repo root.
set -uo pipefail

BASE="https://data.binance.vision/data/futures/um/monthly"
LIST_URL="https://s3-ap-northeast-1.amazonaws.com/data.binance.vision?delimiter=/&prefix=data/futures/um/daily/klines/"
OUT_K="data/listing/klines"
OUT_F="data/listing/funding"
N_MONTHS=2          # first 2 monthly zips of klines cover ~60 days (enough for N<=3,M<=7)
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$OUT_K" "$OUT_F"

echo "[1/3] enumerating all USDT perp symbols ever listed..."
curl -s -m 60 "$LIST_URL" -o "$TMP/list.xml"
grep -o '<Prefix>[^<]*</Prefix>' "$TMP/list.xml" \
  | sed 's/<[^>]*>//g; s#data/futures/um/daily/klines/##; s#/$##' \
  | grep 'USDT$' | sort -u > "$TMP/syms.txt"
TOTAL=$(wc -l < "$TMP/syms.txt" | tr -d ' ')
echo "      $TOTAL USDT perp symbols (incl. delisted)"

# Helper: find the earliest monthly zip (listing month) for a symbol.
# Archive layout: klines is <SYM>/<interval>/ ; fundingRate is just <SYM>/.
# Lists the prefix, greps the first <Key>, extracts YYYY-MM.
first_month() {  # $1=sym $2=kind(klines1d|funding)
  local sym="$1" kind="$2" prefix
  case "$kind" in
    klines1d) prefix="data/futures/um/monthly/klines/${sym}/1d/" ;;
    funding)  prefix="data/futures/um/monthly/fundingRate/${sym}/" ;;
  esac
  curl -s -m 30 "https://s3-ap-northeast-1.amazonaws.com/data.binance.vision?delimiter=/&prefix=${prefix}" \
    | grep -o '<Key>[^<]*</Key>' | sed 's/<[^>]*>//g' | grep -v CHECKSUM \
    | grep -oE '[0-9]{4}-[0-9]{2}' | sort -u | head -1
}

# add N months to a YYYY-MM (portable, no GNU date)
month_seq() {  # $1=YYYY-MM $2=count -> prints count YYYY-MM lines
  local y=${1%-*} m=${1#*-} i n="$2"
  m=$((10#$m))
  for ((i=0;i<n;i++)); do
    printf '%04d-%02d\n' "$y" "$m"
    m=$((m+1)); if ((m>12)); then m=1; y=$((y+1)); fi
  done
}

dl_unzip() {  # $1=url $2=dest_csv (appends, strips header after first)
  local url="$1" dest="$2"
  local z="$TMP/dl.zip"
  if curl -s -m 30 -f "$url" -o "$z" 2>/dev/null; then
    unzip -o -q "$z" -d "$TMP/ex" 2>/dev/null || return 1
    local f
    f=$(ls "$TMP/ex"/*.csv 2>/dev/null | head -1) || return 1
    [ -z "$f" ] && return 1
    cat "$f" >> "$dest"
    rm -f "$TMP/ex"/*.csv
    return 0
  fi
  return 1
}

echo "[2/3] fetching first ${N_MONTHS}mo klines + funding per symbol..."
i=0; got=0; skip=0
while read -r sym; do
  i=$((i+1))
  kdest="$OUT_K/${sym}-1d.csv"
  if [ -s "$kdest" ]; then skip=$((skip+1)); continue; fi

  km=$(first_month "$sym" "klines1d")
  [ -z "$km" ] && { printf '  [%3d/%d] %-16s no klines\n' "$i" "$TOTAL" "$sym"; continue; }

  : > "$kdest"
  kn=0
  while read -r mo; do
    if dl_unzip "${BASE}/klines/${sym}/1d/${sym}-1d-${mo}.zip" "$kdest"; then kn=$((kn+1)); fi
  done < <(month_seq "$km" "$N_MONTHS")
  [ "$kn" -eq 0 ] && { rm -f "$kdest"; printf '  [%3d/%d] %-16s klines empty\n' "$i" "$TOTAL" "$sym"; continue; }

  # funding: grab the first N_MONTHS+1 months (covers any short hold)
  fdest="$OUT_F/${sym}.csv"
  fm=$(first_month "$sym" "funding")
  : > "$fdest"
  if [ -n "$fm" ]; then
    while read -r mo; do
      dl_unzip "${BASE}/fundingRate/${sym}/${sym}-fundingRate-${mo}.zip" "$fdest" >/dev/null
    done < <(month_seq "$fm" $((N_MONTHS+1)))
  fi

  got=$((got+1))
  printf '  [%3d/%d] %-16s klines=%d funding=%s\n' "$i" "$TOTAL" "$sym" "$kn" \
    "$( [ -s "$fdest" ] && echo y || echo n )"
done < "$TMP/syms.txt"

echo "[3/3] done. fetched=$got skipped=$skip total=$TOTAL"
echo "      klines -> $OUT_K/  funding -> $OUT_F/"
