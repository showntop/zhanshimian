#!/bin/sh
set -eu

api_base="${API_BASE_URL:-http://127.0.0.1:58000}"

login_json="$(curl -fsS -X POST "$api_base/v1/auth/dev" -H 'content-type: application/json' -d '{"nickname":"端到端测试"}')"
token="$(printf '%s' "$login_json" | jq -r '.data.token')"

media_ids=""
face_media_id=""
for kind in face side body; do
  media_json="$(curl -fsS -X POST "$api_base/v1/media/demo" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"kind\":\"$kind\"}")"
  media_id="$(printf '%s' "$media_json" | jq -r '.data.id')"
	if [ "$kind" = "face" ]; then face_media_id="$media_id"; fi
  if [ -z "$media_ids" ]; then media_ids="\"$media_id\""; else media_ids="$media_ids,\"$media_id\""; fi
done

analysis_json="$(curl -fsS -X POST "$api_base/v1/analyses" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"scene\":\"interview\",\"media_ids\":[$media_ids],\"profile\":{\"height_cm\":165,\"role\":\"产品经理\",\"budget\":\"500-1500\"}}")"
analysis_id="$(printf '%s' "$analysis_json" | jq -r '.data.id')"

report_id=""
attempt=0
while [ "$attempt" -lt 20 ]; do
  status_json="$(curl -fsS "$api_base/v1/analyses/$analysis_id" -H "Authorization: Bearer $token")"
  status="$(printf '%s' "$status_json" | jq -r '.data.status')"
  if [ "$status" = "completed" ]; then report_id="$(printf '%s' "$status_json" | jq -r '.data.report_id')"; break; fi
  if [ "$status" = "failed" ]; then printf '%s\n' "$status_json"; exit 1; fi
  attempt=$((attempt + 1))
  sleep 1
done
test -n "$report_id"

report_json="$(curl -fsS "$api_base/v1/reports/$report_id" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$report_json" | jq '.data.findings | length')" -eq 4

plans_json="$(curl -fsS "$api_base/v1/reports/$report_id/plans" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$plans_json" | jq '.data | length')" -eq 3
plan_id="$(printf '%s' "$plans_json" | jq -r '.data[0].id')"

curl -fsS -X POST "$api_base/v1/plans/$plan_id/select" -H "Authorization: Bearer $token" >/dev/null
checklist_json="$(curl -fsS "$api_base/v1/plans/$plan_id/checklist" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$checklist_json" | jq '.data | length')" -eq 3
item_id="$(printf '%s' "$checklist_json" | jq -r '.data[0].id')"
curl -fsS -X PATCH "$api_base/v1/checklist/$item_id" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"completed":true}' >/dev/null
curl -fsS -X POST "$api_base/v1/feedback" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"plan_id\":\"$plan_id\",\"tags\":[\"更有精神\"],\"comment\":\"端到端验证\"}" >/dev/null

hair_json="$(curl -fsS -X POST "$api_base/v1/tools/run" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"kind\":\"hair\",\"report_id\":\"$report_id\",\"scene\":\"daily\"}")"
test "$(printf '%s' "$hair_json" | jq '.data.options | length')" -eq 3
hair_result_id="$(printf '%s' "$hair_json" | jq -r '.data.id')"
hair_saved_json="$(curl -fsS -X POST "$api_base/v1/tools/$hair_result_id/save" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$hair_saved_json" | jq -r '.data.saved')" = "true"

preview_json="$(curl -fsS -X POST "$api_base/v1/hair-previews" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"media_id\":\"$face_media_id\",\"report_id\":\"$report_id\",\"style_id\":\"sharp\",\"scene\":\"daily\"}")"
preview_id="$(printf '%s' "$preview_json" | jq -r '.data.id')"
preview_status=""
attempt=0
while [ "$attempt" -lt 20 ]; do
  preview_json="$(curl -fsS "$api_base/v1/hair-previews/$preview_id" -H "Authorization: Bearer $token")"
  preview_status="$(printf '%s' "$preview_json" | jq -r '.data.status')"
  if [ "$preview_status" = "completed" ]; then break; fi
  if [ "$preview_status" = "failed" ]; then printf '%s\n' "$preview_json"; exit 1; fi
  attempt=$((attempt + 1))
  sleep 1
done
test "$preview_status" = "completed"
test -n "$(printf '%s' "$preview_json" | jq -r '.data.result_image_url')"
preview_saved_json="$(curl -fsS -X POST "$api_base/v1/hair-previews/$preview_id/save" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$preview_saved_json" | jq -r '.data.saved')" = "true"
saved_previews_json="$(curl -fsS "$api_base/v1/hair-previews" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$saved_previews_json" | jq '.data | length')" -ge 1

outfit_media_json="$(curl -fsS -X POST "$api_base/v1/media/demo" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"kind":"outfit"}')"
outfit_media_id="$(printf '%s' "$outfit_media_json" | jq -r '.data.id')"
other_login_json="$(curl -fsS -X POST "$api_base/v1/auth/dev" -H 'content-type: application/json' -d '{"nickname":"越权校验"}')"
other_token="$(printf '%s' "$other_login_json" | jq -r '.data.token')"
cross_user_status="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$api_base/v1/tools/run" -H "Authorization: Bearer $other_token" -H 'content-type: application/json' -d "{\"kind\":\"outfit\",\"media_id\":\"$outfit_media_id\",\"scene\":\"daily\"}")"
test "$cross_user_status" = "404"
outfit_json="$(curl -fsS -X POST "$api_base/v1/tools/run" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"kind\":\"outfit\",\"media_id\":\"$outfit_media_id\",\"report_id\":\"$report_id\",\"scene\":\"interview\"}")"
test "$(printf '%s' "$outfit_json" | jq '.data.findings | length')" -eq 3
test "$(printf '%s' "$outfit_json" | jq -r '.data.provider_version')" = "demo-outfit-v1"
outfit_result_id="$(printf '%s' "$outfit_json" | jq -r '.data.id')"
outfit_saved_json="$(curl -fsS -X POST "$api_base/v1/tools/$outfit_result_id/save" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$outfit_saved_json" | jq -r '.data.saved')" = "true"

product_media_json="$(curl -fsS -X POST "$api_base/v1/media/demo" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"kind":"product"}')"
product_media_id="$(printf '%s' "$product_media_json" | jq -r '.data.id')"
purchase_json="$(curl -fsS -X POST "$api_base/v1/tools/run" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"kind\":\"purchase\",\"media_id\":\"$product_media_id\",\"report_id\":\"$report_id\",\"scene\":\"date\"}")"
test -n "$(printf '%s' "$purchase_json" | jq -r '.data.conclusion')"
purchase_result_id="$(printf '%s' "$purchase_json" | jq -r '.data.id')"
purchase_saved_json="$(curl -fsS -X POST "$api_base/v1/tools/$purchase_result_id/save" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$purchase_saved_json" | jq -r '.data.saved')" = "true"

printf 'e2e passed: report=4 findings, plans=3, checklist=3, async hair preview and advisor tools persisted\n'
