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

curl -fsS -X POST "$api_base/v1/events" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"name":"page_view","payload":{"page":"e2e"}}' | jq -e '.data.accepted == true' >/dev/null
today_context="$(curl -fsS "$api_base/v1/today/context?city=%E6%9D%AD%E5%B7%9E&schedule=%E9%80%9A%E5%8B%A4" -H "Authorization: Bearer $token" | jq -c '.data')"
test "$(printf '%s' "$today_context" | jq -r '.city')" = "杭州"
today_plan="$(curl -fsS -X POST "$api_base/v1/today/plans" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"report_id\":\"$report_id\",\"city\":\"杭州\",\"schedule\":\"通勤\"}" | jq -c '.data')"
today_plan_id="$(printf '%s' "$today_plan" | jq -r '.id')"
curl -fsS -X POST "$api_base/v1/today/plans/$today_plan_id/activate" -H "Authorization: Bearer $token" | jq -e '.data.active == true' >/dev/null
curl -fsS -X POST "$api_base/v1/today/plans/$today_plan_id/feedback" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"feedback":"适合我"}' | jq -e '.data.feedback == "适合我"' >/dev/null

share_card="$(curl -fsS -X POST "$api_base/v1/share-cards" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"source_type\":\"today\",\"source_id\":\"$today_plan_id\",\"include_photo\":false}" | jq -c '.data')"
share_id="$(printf '%s' "$share_card" | jq -r '.id')"
share_token="$(printf '%s' "$share_card" | jq -r '.token')"
curl -fsS "$api_base/v1/share/$share_token" | jq -e '.data.snapshot.title | length > 0' >/dev/null
curl -fsS -X POST "$api_base/v1/share-cards/$share_id/revoke" -H "Authorization: Bearer $token" | jq -e '.data.revoked == true' >/dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/share/$share_token")" = "404"

curl -fsS -X POST "$api_base/v1/wardrobe/items" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"name":"针织衫","category":"top","color":"米白","scenes":["daily"]}' >/dev/null
curl -fsS -X POST "$api_base/v1/wardrobe/items" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"name":"长裤","category":"bottom","color":"藏蓝","scenes":["daily"]}' >/dev/null
wardrobe_outfit="$(curl -fsS -X POST "$api_base/v1/wardrobe/outfits" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "$today_context" | jq -c '.data')"
wardrobe_outfit_id="$(printf '%s' "$wardrobe_outfit" | jq -r '.id')"
worn_outfit="$(curl -fsS -X POST "$api_base/v1/wardrobe/outfits/$wardrobe_outfit_id/wear" -H "Authorization: Bearer $token" | jq -c '.data')"
printf '%s' "$worn_outfit" | jq -e '.worn == true and (.items | length) == 2 and ([.items[].wear_count] | min) >= 1' >/dev/null

advisor_message="$(curl -fsS -X POST "$api_base/v1/advisor/messages" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"content\":\"只用现有衣橱\",\"report_id\":\"$report_id\",\"today_plan_id\":\"$today_plan_id\"}" | jq -c '.data')"
printf '%s' "$advisor_message" | jq -e '(.content | contains("米白针织衫")) and (.content | contains("藏蓝长裤")) and (.actions | length) == 1' >/dev/null
advisor_action_id="$(printf '%s' "$advisor_message" | jq -r '.actions[0].id')"
curl -fsS -X POST "$api_base/v1/advisor/actions/$advisor_action_id/apply" -H "Authorization: Bearer $token" | jq -e '.data.applied == true' >/dev/null

privacy_share="$(curl -fsS -X POST "$api_base/v1/share-cards" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"source_type\":\"today\",\"source_id\":\"$today_plan_id\",\"include_photo\":false}" | jq -r '.data.token')"
curl -fsS -X DELETE "$api_base/v1/me/data" -H "Authorization: Bearer $token" -o /dev/null
test "$(curl -fsS "$api_base/v1/today/plans/current" -H "Authorization: Bearer $token" | jq -r '.data')" = "null"
test "$(curl -fsS "$api_base/v1/wardrobe/items" -H "Authorization: Bearer $token" | jq '.data | length')" -eq 0
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/share/$privacy_share")" = "404"

printf 'e2e passed: analysis, scene plans, tools, today, share, wardrobe, grounded advisor, events and privacy deletion\n'
