#!/bin/sh
# 每日内容端到端回归：prepare → generate（永远 200）→ 幂等 → 收藏全生命周期。
# 前置：本地 API 已启动（make up 或本地 go run），DEV 登录可用。
# 通过时最后一行输出: daily e2e passed
set -eu

api_base="${API_BASE_URL:-http://127.0.0.1:58000}"

# DEV 登录拿 token（微信登录不可用的本地环境）。
token() {
  curl -fsS -X POST "$api_base/v1/auth/dev" \
    -H 'content-type: application/json' \
    -d '{"nickname":"daily-e2e"}' \
    | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["token"])'
}

TOKEN="$(token)"

fail() { echo "daily e2e FAILED: $1" >&2; exit 1; }

# ① prepare：返回 gen_date / scenario / 一次性 pick_token。
prepare="$(curl -fsS -X POST "$api_base/v1/daily/prepare" \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' -d '{}')"
echo "$prepare" | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];assert d["gen_date"] and d["scenario"]' \
  || fail "prepare shape"
cache_hit="$(echo "$prepare" | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["cache_hit"])')"
pick_token="$(echo "$prepare" | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["pick_token"])')"

# ② generate：永远 200，source ∈ {generated, fallback}，字段完整。
gen="$(curl -fsS -X POST "$api_base/v1/daily/generate" \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d "{\"pick_token\":\"$pick_token\"}")"
summary="$(echo "$gen" | python3 -c '
import json,sys
d = json.load(sys.stdin)["data"]
assert d["source"] in ("generated", "fallback"), d["source"]
c = d["content"]
for field in ("id", "type", "topic", "lead", "fit_text", "why", "visual", "asset", "dedupe_key"):
    assert c[field] not in (None, ""), field
v = c["visual"]
assert v["modality"] and v["alt"] is not None and isinstance(v["spec"], dict)
print(d["source"], c["id"], c["dedupe_key"])
')" || fail "generate shape: $gen"
echo "generate: $summary"

content_id="$(echo "$summary" | cut -d' ' -f2)"

# ③ 幂等：再 generate 一次，返回同一条（(user_id, gen_date) 唯一）。
gen2="$(curl -fsS -X POST "$api_base/v1/daily/generate" \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' -d '{"pick_token":""}')"
echo "$gen2" | python3 -c "
import json,sys
d = json.load(sys.stdin)['data']
assert d['content']['id'] == '$content_id', d['content']['id']
" || fail "generate idempotency"

# ④ 收下：副本 + 素材 + 语境；重复收下返回同一条。
col="$(curl -fsS -X POST "$api_base/v1/daily/collection" \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d "{\"content_id\":\"$content_id\",\"note\":\"先留着\"}")"
col_summary="$(echo "$col" | python3 -c '
import json,sys
d = json.load(sys.stdin)["data"]
assert d["content_key"], "content_key"
s = d["content_snapshot"]
for field in ("id", "type", "topic", "lead", "fitText", "why", "visual", "category"):
    assert s[field] not in (None, ""), field
assert d["status"] == "saved"
assert isinstance(d["assets"], list)
print(d["id"])
')" || fail "collection shape: $col"
col_id="$(echo "$col_summary")"
col2="$(curl -fsS -X POST "$api_base/v1/daily/collection" \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d "{\"content_id\":\"$content_id\"}")"
echo "$col2" | python3 -c "
import json,sys
assert json.load(sys.stdin)['data']['id'] == '$col_id'
" || fail "collection idempotency"

# ⑤ 统计 / 生命周期 / 列表 / 移出。
stats="$(curl -fsS "$api_base/v1/daily/collection/stats" -H "Authorization: Bearer $TOKEN")"
echo "$stats" | python3 -c '
import json,sys
d = json.load(sys.stdin)["data"]
assert set(d["counts"]) == {"color","fit","proportion","fabric","occasion","howto","outfit"}
assert d["total"] >= 1
' || fail "stats shape: $stats"

patched="$(curl -fsS -X PATCH "$api_base/v1/daily/collection/$col_id" \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"status":"kept"}')"
echo "$patched" | python3 -c '
import json,sys
d = json.load(sys.stdin)["data"]
assert d["status"] == "kept"
' || fail "patch"

listed="$(curl -fsS "$api_base/v1/daily/collection?category=color" -H "Authorization: Bearer $TOKEN")"
echo "$listed" | python3 -c '
import json,sys
d = json.load(sys.stdin)["data"]
assert len(d) >= 1
' || fail "list"

code="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$api_base/v1/daily/collection/$col_id" \
  -H "Authorization: Bearer $TOKEN")"
[ "$code" = "204" ] || fail "delete status $code"
code2="$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$api_base/v1/daily/collection/$col_id" \
  -H "Authorization: Bearer $TOKEN")"
[ "$code2" = "204" ] || fail "delete idempotency status $code2"

# ⑥ 越权：别人的收藏不可改（404；本契约没有按 id 读的端点）。
other="$(token)"
code3="$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$api_base/v1/daily/collection/$col_id" \
  -H "Authorization: Bearer $other" -H 'content-type: application/json' -d '{"status":"tried"}')"
[ "$code3" = "404" ] || fail "cross-user patch expected 404, got $code3"

echo "daily e2e passed"
