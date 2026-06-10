#!/usr/bin/env bash
# fetch_chunk.sh <symfile> — fetch listing data for symbols in <symfile> (one/line).
# Same logic as scripts/fetch_listing_data.sh but reads an explicit symbol list so
# multiple workers can run on disjoint chunks. Idempotent (skips existing klines).
set -uo pipefail
SYMFILE="$1"
BASE="https://data.binance.vision/data/futures/um/monthly"
OUT_K="data/listing/klines"; OUT_F="data/listing/funding"
N_MONTHS=2
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$OUT_K" "$OUT_F" "$TMP/ex"

first_month() {  # $1=sym $2=klines1d|funding
  local sym="$1" kind="$2" prefix=""
  case "$kind" in
    klines1d) prefix="data/futures/um/monthly/klines/${sym}/1d/" ;;
    funding)  prefix="data/futures/um/monthly/fundingRate/${sym}/" ;;
  esac
  curl -s -m 30 "https://s3-ap-northeast-1.amazonaws.com/data.binance.vision?delimiter=/&prefix=${prefix}" \
    | grep -o '<Key>[^<]*</Key>' | sed 's/<[^>]*>//g' | grep -v CHECKSUM \
    | grep -oE '[0-9]{4}-[0-9]{2}' | sort -u | head -1
}
month_seq() { local y=${1%-*} m=${1#*-} i n="$2"; m=$((10#$m))
  for ((i=0;i<n;i++)); do printf '%04d-%02d\n' "$y" "$m"; m=$((m+1)); if ((m>12)); then m=1; y=$((y+1)); fi; done; }
dl_unzip() { local url="$1" dest="$2" z="$TMP/dl$$.zip"
  if curl -s -m 30 -f "$url" -o "$z" 2>/dev/null; then
    unzip -o -q "$z" -d "$TMP/ex" 2>/dev/null || return 1
    local f; f=$(ls "$TMP/ex"/*.csv 2>/dev/null | head -1); [ -z "$f" ] && return 1
    cat "$f" >> "$dest"; rm -f "$TMP/ex"/*.csv; return 0
  fi; return 1; }

while read -r sym; do
  [ -z "$sym" ] && continue
  kdest="$OUT_K/${sym}-1d.csv"
  [ -s "$kdest" ] && continue
  km=$(first_month "$sym" "klines1d"); [ -z "$km" ] && continue
  : > "$kdest"; kn=0
  while read -r mo; do dl_unzip "${BASE}/klines/${sym}/1d/${sym}-1d-${mo}.zip" "$kdest" && kn=$((kn+1)); done < <(month_seq "$km" "$N_MONTHS")
  [ "$kn" -eq 0 ] && { rm -f "$kdest"; continue; }
  fdest="$OUT_F/${sym}.csv"; fm=$(first_month "$sym" "funding"); : > "$fdest"
  if [ -n "$fm" ]; then while read -r mo; do dl_unzip "${BASE}/fundingRate/${sym}/${sym}-fundingRate-${mo}.zip" "$fdest" >/dev/null; done < <(month_seq "$fm" $((N_MONTHS+1))); fi
  echo "$sym done (k=$kn)"
done < "$SYMFILE"
echo "CHUNK COMPLETE: $SYMFILE"
