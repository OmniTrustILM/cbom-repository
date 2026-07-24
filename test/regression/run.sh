#!/usr/bin/env bash
# Regression checks against a running cbom-repository (see docker-compose.yml).
# Exercises 1.6 + 1.7 upload/get/versions/search plus the negative paths and a
# simulation of core's CbomRepositoryClient request shape.
set -u

BASE="${BASE:-http://localhost:8080/api}"
DIR="$(cd "$(dirname "$0")" && pwd)"
CT="application/vnd.cyclonedx+json"
pass=0; fail=0

# check NAME EXPECTED_CODE  (curl args...)  -> asserts HTTP status
check() {
  local name=$1 want=$2; shift 2
  local code; code=$(curl -s -o /tmp/reg_body.$$ -w '%{http_code}' "$@")
  if [ "$code" = "$want" ]; then
    echo "  PASS  $name  (HTTP $code)"; pass=$((pass+1))
  else
    echo "  FAIL  $name  (want $want, got $code)"; echo "        body: $(head -c 300 /tmp/reg_body.$$)"; fail=$((fail+1))
  fi
}

echo "== waiting for cbom-repository at $BASE =="
for i in $(seq 1 60); do
  curl -sf "$BASE/v1/health/liveness" >/dev/null 2>&1 && break
  sleep 2
done
curl -sf "$BASE/v1/health/liveness" >/dev/null 2>&1 || { echo "service not healthy"; exit 1; }
echo "healthy."

echo "== positive: uploads =="
check "upload 1.6 (version=1.6)"            201 -X POST "$BASE/v1/bom" -H "Content-Type: $CT; version=1.6" --data-binary @"$DIR/samples/bom-1.6.json"
SN16=$(sed -n 's/.*"serialNumber":"\([^"]*\)".*/\1/p' /tmp/reg_body.$$)
check "upload 1.7 (version=1.7)"            201 -X POST "$BASE/v1/bom" -H "Content-Type: $CT; version=1.7" --data-binary @"$DIR/samples/bom-1.7.json"
SN17=$(sed -n 's/.*"serialNumber":"\([^"]*\)".*/\1/p' /tmp/reg_body.$$)
echo "  (1.6 serial=$SN16 ; 1.7 serial=$SN17)"

echo "== positive: retrieval / versions / search =="
[ -n "$SN17" ] && check "get 1.7 by urn"    200 "$BASE/v1/bom/$SN17"
[ -n "$SN17" ] && { curl -s "$BASE/v1/bom/$SN17" | grep -q '"specVersion": *"1.7"' && { echo "  PASS  served 1.7 body preserved"; pass=$((pass+1)); } || { echo "  FAIL  served 1.7 body missing specVersion 1.7"; fail=$((fail+1)); }; }
[ -n "$SN17" ] && check "list versions"     200 "$BASE/v1/bom/$SN17/versions"
check "search (after=0)"                    200 "$BASE/v1/bom?after=0"

echo "== negative: validation / classification =="
check "version/body mismatch -> 400"        400 -X POST "$BASE/v1/bom" -H "Content-Type: $CT; version=1.6" --data-binary @"$DIR/samples/bom-1.7.json"
check "unsupported version 1.4 -> 400"      400 -X POST "$BASE/v1/bom" -H "Content-Type: $CT; version=1.4" --data-binary @"$DIR/samples/bom-1.6.json"
check "unknown specVersion 1.8 -> 400"      400 -X POST "$BASE/v1/bom" -H "Content-Type: $CT; version=1.7" --data-binary '{"bomFormat":"CycloneDX","specVersion":"1.8"}'
check "wrong base media type -> 415"        415 -X POST "$BASE/v1/bom" -H "Content-Type: application/json" --data-binary @"$DIR/samples/bom-1.6.json"

echo "== integration: simulate core's CbomRepositoryClient (no version= param) =="
# core sends Content-Type WITHOUT a version parameter -> the server auto-detects the
# version from the document's specVersion, so both 1.6 and 1.7 bodies are accepted.
check "core-shape + 1.6 body -> 201 (auto-detected)"  201 -X POST "$BASE/v1/bom" -H "Content-Type: $CT" --data-binary @"$DIR/samples/bom-1.6.json"
check "core-shape + 1.7 body -> 201 (auto-detected)"  201 -X POST "$BASE/v1/bom" -H "Content-Type: $CT" --data-binary @"$DIR/samples/bom-1.7.json"

echo "== optional: oversized body -> 413 (set REG_OVERSIZE=1 to run; generates ~21MiB) =="
if [ "${REG_OVERSIZE:-0}" = "1" ]; then
  big=/tmp/reg_big.$$; { printf '{"bomFormat":"CycloneDX","specVersion":"1.7","x":"'; head -c 22000000 /dev/zero | tr '\0' 'a'; printf '"}'; } > "$big"
  check "oversized body -> 413"             413 -X POST "$BASE/v1/bom" -H "Content-Type: $CT; version=1.7" --data-binary @"$big"
  rm -f "$big"
fi

rm -f /tmp/reg_body.$$
echo ""
echo "== summary: $pass passed, $fail failed =="
exit $([ "$fail" -eq 0 ] && echo 0 || echo 1)
