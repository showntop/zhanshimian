#!/bin/sh
# 新契约（contracts/openapi.yaml）端到端回归。
# 覆盖：主闭环、统一任务轮询、PUT plans 幂等、单方案重试、诊断/发型、
# 首页聚合、今日、分享、衣橱（显式 item_ids）、顾问、sms 登录与跨次同号互通、
# 越权 404、删除完整性。
set -eu

api_base="${API_BASE_URL:-http://127.0.0.1:58000}"

# 轮询统一任务直到终态；echo 任务 JSON。
wait_task() {
  task_id="$1"; label="$2"; attempt=0
  while [ "$attempt" -lt 30 ]; do
    task_json="$(curl -fsS "$api_base/v1/tasks/$task_id" -H "Authorization: Bearer $token")"
    status="$(printf '%s' "$task_json" | jq -r '.data.status')"
    if [ "$status" = "completed" ] || [ "$status" = "failed" ]; then
      if [ "$status" = "failed" ]; then printf '%s %s\n' "$label" "$task_json"; exit 1; fi
      printf '%s' "$task_json"; return
    fi
    attempt=$((attempt + 1)); sleep 1
  done
  echo "$label: task $task_id 轮询超时" >&2; exit 1
}

login_json="$(curl -fsS -X POST "$api_base/v1/auth/dev" -H 'content-type: application/json' -d '{"nickname":"端到端测试"}')"
token="$(printf '%s' "$login_json" | jq -r '.data.token')"

# ---- 补充资料（me/profile 持久化） ----
curl -fsS -X PUT "$api_base/v1/me/profile" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d '{"height_cm":165,"role":"产品经理","budget":"500-1500"}' | jq -e '.data.height_cm == 165' >/dev/null
curl -fsS "$api_base/v1/me/profile" -H "Authorization: Bearer $token" | jq -e '.data.role == "产品经理"' >/dev/null
me_id="$(curl -fsS "$api_base/v1/me" -H "Authorization: Bearer $token" | jq -r '.data.id')"

# ---- 三图 → 异步分析（统一任务轮询） ----
media_ids=""
face_media_id=""
for kind in face side body; do
  media_id="$(curl -fsS -X POST "$api_base/v1/media/demo" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "{\"kind\":\"$kind\"}" | jq -r '.data.id')"
  if [ "$kind" = "face" ]; then face_media_id="$media_id"; fi
  if [ -z "$media_ids" ]; then media_ids="\"$media_id\""; else media_ids="$media_ids,\"$media_id\""; fi
done

analysis_json="$(curl -fsS -X POST "$api_base/v1/analyses" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"scene\":\"interview\",\"media_ids\":[$media_ids],\"profile\":{\"height_cm\":165,\"role\":\"产品经理\",\"budget\":\"500-1500\"}}")"
analysis_id="$(printf '%s' "$analysis_json" | jq -r '.data.id')"
analysis_task_id="$(printf '%s' "$analysis_json" | jq -r '.task.id')"
test -n "$analysis_task_id"
wait_task "$analysis_task_id" "analysis" | jq -e '.data.status == "completed" and .data.progress == 100' >/dev/null

report_id="$(curl -fsS "$api_base/v1/analyses/$analysis_id" -H "Authorization: Bearer $token" | jq -r '.data.report_id')"
test -n "$report_id"
curl -fsS "$api_base/v1/reports/$report_id" -H "Authorization: Bearer $token" | jq -e '.data.findings | length == 4' >/dev/null
curl -fsS "$api_base/v1/reports/current" -H "Authorization: Bearer $token" | jq -e ".data.id == \"$report_id\"" >/dev/null

# ---- PUT plans：幂等创建-或-刷新（同参数二次 PUT 不重建） ----
put_body='{"scene":"interview","answers":{"when":"week","format":"onsite","preparation":"key-piece","impression":"natural"}}'
plans_json="$(curl -fsS -X PUT "$api_base/v1/reports/$report_id/plans" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "$put_body")"
test "$(printf '%s' "$plans_json" | jq '.data | length')" -eq 3
plan_id="$(printf '%s' "$plans_json" | jq -r '.data[0].id')"
plan_name="$(printf '%s' "$plans_json" | jq -r '.data[0].name')"
plans_again="$(curl -fsS -X PUT "$api_base/v1/reports/$report_id/plans" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d "$put_body")"
test "$(printf '%s' "$plans_again" | jq -r '.data[0].id')" = "$plan_id"
# scene 过滤：interview 组与 general 组互不混
curl -fsS "$api_base/v1/reports/$report_id/plans?scene=interview" -H "Authorization: Bearer $token" | jq -e '.data | length == 3' >/dev/null

# ---- 单方案图重试（202 + task） ----
regen_json="$(curl -fsS -X POST "$api_base/v1/plans/$plan_id/look/regenerate" -H "Authorization: Bearer $token")"
regen_task_id="$(printf '%s' "$regen_json" | jq -r '.task.id')"
test -n "$regen_task_id"
wait_task "$regen_task_id" "plan-look" >/dev/null
curl -fsS "$api_base/v1/plans/$plan_id" -H "Authorization: Bearer $token" | jq -e ".data.id == \"$plan_id\" and (.data.generated_image_url | length > 0)" >/dev/null

# ---- 批量任务状态 ----
curl -fsS "$api_base/v1/tasks?ids=$analysis_task_id,$regen_task_id" -H "Authorization: Bearer $token" | jq -e '.data | length == 2' >/dev/null

# ---- 选定 → 清单（嵌套路由）→ 反馈（G3 文案） ----
curl -fsS -X POST "$api_base/v1/plans/$plan_id/select" -H "Authorization: Bearer $token" >/dev/null
checklist_json="$(curl -fsS "$api_base/v1/plans/$plan_id/checklist" -H "Authorization: Bearer $token")"
test "$(printf '%s' "$checklist_json" | jq '.data | length')" -eq 3
item_id="$(printf '%s' "$checklist_json" | jq -r '.data[0].id')"
curl -fsS -X PATCH "$api_base/v1/plans/$plan_id/checklist/$item_id" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"completed":true}' | jq -e '.data.completed == true' >/dev/null
feedback_json="$(curl -fsS -X POST "$api_base/v1/plans/$plan_id/feedback" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"tags":["更有精神"],"comment":"端到端验证"}')"
printf '%s' "$feedback_json" | jq -e ".data.saved == true and (.data.message | length > 0)" >/dev/null

# ---- 发型推荐与预览 ----
curl -fsS "$api_base/v1/hairstyles?report_id=$report_id" -H "Authorization: Bearer $token" | jq -e '.data | length == 3' >/dev/null
preview_json="$(curl -fsS -X POST "$api_base/v1/hair-previews" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"media_id\":\"$face_media_id\",\"report_id\":\"$report_id\",\"style_id\":\"sharp\",\"scene\":\"daily\"}")"
preview_id="$(printf '%s' "$preview_json" | jq -r '.data.id')"
preview_task_id="$(printf '%s' "$preview_json" | jq -r '.task.id')"
wait_task "$preview_task_id" "hair-preview" >/dev/null
preview_final="$(curl -fsS "$api_base/v1/hair-previews/$preview_id" -H "Authorization: Bearer $token")"
printf '%s' "$preview_final" | jq -e '.data.status == "completed" and (.data.result_image_url | length > 0)' >/dev/null
curl -fsS -X POST "$api_base/v1/hair-previews/$preview_id/save" -H "Authorization: Bearer $token" | jq -e '.data.saved == true' >/dev/null
curl -fsS "$api_base/v1/hair-previews?saved=true" -H "Authorization: Bearer $token" | jq -e '.data | length >= 1' >/dev/null

# ---- 诊断（outfit/purchase）+ 越权校验 ----
outfit_media_id="$(curl -fsS -X POST "$api_base/v1/media/demo" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"kind":"outfit"}' | jq -r '.data.id')"
other_token="$(curl -fsS -X POST "$api_base/v1/auth/dev" -H 'content-type: application/json' -d '{"nickname":"越权校验"}' | jq -r '.data.token')"
cross_user_status="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$api_base/v1/diagnostics" -H "Authorization: Bearer $other_token" -H 'content-type: application/json' \
  -d "{\"kind\":\"outfit\",\"media_id\":\"$outfit_media_id\",\"scene\":\"daily\"}")"
test "$cross_user_status" = "404"
outfit_json="$(curl -fsS -X POST "$api_base/v1/diagnostics" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"kind\":\"outfit\",\"media_id\":\"$outfit_media_id\",\"report_id\":\"$report_id\",\"scene\":\"interview\"}")"
printf '%s' "$outfit_json" | jq -e '.data.findings | length == 3' >/dev/null
outfit_result_id="$(printf '%s' "$outfit_json" | jq -r '.data.id')"
curl -fsS -X PATCH "$api_base/v1/diagnostics/$outfit_result_id" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"saved":true}' | jq -e '.data.saved == true' >/dev/null
product_media_id="$(curl -fsS -X POST "$api_base/v1/media/demo" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"kind":"product"}' | jq -r '.data.id')"
purchase_json="$(curl -fsS -X POST "$api_base/v1/diagnostics" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"kind\":\"purchase\",\"media_id\":\"$product_media_id\",\"report_id\":\"$report_id\",\"scene\":\"date\"}")"
printf '%s' "$purchase_json" | jq -e '.data.conclusion | length > 0' >/dev/null

# ---- 首页聚合（单请求） ----
curl -fsS "$api_base/v1/home/bootstrap" -H "Authorization: Bearer $token" \
  | jq -e ".data.report.id == \"$report_id\" and (.data.active_tasks | type == \"array\") and (.data.profile_summary.height_cm == 165)" >/dev/null

# ---- 埋点 + 今日全流程 ----
curl -fsS -X POST "$api_base/v1/events" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"name":"page_view","payload":{"page":"e2e"}}' | jq -e '.data.accepted == true' >/dev/null
today_context="$(curl -fsS "$api_base/v1/today/context?city=%E6%9D%AD%E5%B7%9E&schedule=%E9%80%9A%E5%8B%A4" -H "Authorization: Bearer $token" | jq -c '.data')"
test "$(printf '%s' "$today_context" | jq -r '.city')" = "杭州"
today_plan="$(curl -fsS -X POST "$api_base/v1/today/plans" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"report_id\":\"$report_id\",\"city\":\"杭州\",\"schedule\":\"通勤\"}" | jq -c '.data')"
today_plan_id="$(printf '%s' "$today_plan" | jq -r '.id')"
curl -fsS -X POST "$api_base/v1/today/plans/$today_plan_id/activate" -H "Authorization: Bearer $token" | jq -e '.data.active == true' >/dev/null
curl -fsS -X POST "$api_base/v1/today/plans/$today_plan_id/feedback" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"feedback":"适合我"}' | jq -e '.data.feedback == "适合我"' >/dev/null

# ---- 分享：创建 → 公开读 → DELETE → 404 ----
share_card="$(curl -fsS -X POST "$api_base/v1/shares" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"source_type\":\"today\",\"source_id\":\"$today_plan_id\",\"include_photo\":false}")"
share_id="$(printf '%s' "$share_card" | jq -r '.data.id')"
share_token="$(printf '%s' "$share_card" | jq -r '.data.token')"
curl -fsS "$api_base/v1/shares/$share_token" | jq -e '.data.snapshot.title | length > 0' >/dev/null
curl -fsS -X DELETE "$api_base/v1/shares/$share_id" -H "Authorization: Bearer $token" | jq -e '.data.revoked == true' >/dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/shares/$share_token")" = "404"

# ---- 衣橱（显式 item_ids 组合） ----
item1="$(curl -fsS -X POST "$api_base/v1/wardrobe/items" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"name":"针织衫","category":"top","color":"米白","scenes":["daily"]}' | jq -r '.data.id')"
item2="$(curl -fsS -X POST "$api_base/v1/wardrobe/items" -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"name":"长裤","category":"bottom","color":"藏蓝","scenes":["daily"]}' | jq -r '.data.id')"
wardrobe_outfit_id="$(curl -fsS -X POST "$api_base/v1/wardrobe/outfits" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"title\":\"通勤组合\",\"item_ids\":[\"$item1\",\"$item2\"],\"context\":$today_context}" | jq -r '.data.id')"
worn_outfit="$(curl -fsS -X POST "$api_base/v1/wardrobe/outfits/$wardrobe_outfit_id/wear" -H "Authorization: Bearer $token" | jq -c '.data')"
printf '%s' "$worn_outfit" | jq -e '.worn == true and (.items | length) == 2 and ([.items[].wear_count] | min) >= 1' >/dev/null

# ---- 顾问（grounding 命中衣橱单品） ----
advisor_message="$(curl -fsS -X POST "$api_base/v1/advisor/messages" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"content\":\"只用现有衣橱\",\"report_id\":\"$report_id\",\"today_plan_id\":\"$today_plan_id\"}" | jq -c '.data')"
printf '%s' "$advisor_message" | jq -e '(.content | contains("米白针织衫")) and (.content | contains("藏蓝长裤")) and (.actions | length) == 1' >/dev/null
advisor_action_id="$(printf '%s' "$advisor_message" | jq -r '.actions[0].id')"
curl -fsS -X POST "$api_base/v1/advisor/actions/$advisor_action_id/apply" -H "Authorization: Bearer $token" | jq -e '.data.applied == true' >/dev/null

# ---- sms 登录（console dev_code）→ 同号二次登录数据互通 ----
# 每次运行使用唯一手机号，避免反复 e2e 触发 5/小时/号码 的合法限流。
phone="138$(printf '%08d' $(( $(date +%s) % 100000000 )))"
sms_json="$(curl -fsS -X POST "$api_base/v1/auth/sms/request" -H 'content-type: application/json' -d "{\"phone\":\"$phone\"}")"
dev_code="$(printf '%s' "$sms_json" | jq -r '.data.dev_code')"
test -n "$dev_code"
sms_session="$(curl -fsS -X POST "$api_base/v1/auth/sms/verify" -H 'content-type: application/json' -d "{\"phone\":\"$phone\",\"code\":\"$dev_code\"}")"
sms_token="$(printf '%s' "$sms_session" | jq -r '.data.token')"
test "$(curl -fsS "$api_base/v1/me" -H "Authorization: Bearer $sms_token" | jq -r '.data.identities | length')" -ge 1
# 验证码已使用：同号可立即再取（60s 冷却只约束未使用的上一条码）
sms2_json="$(curl -fsS -X POST "$api_base/v1/auth/sms/request" -H 'content-type: application/json' -d "{\"phone\":\"$phone\"}")"
dev_code2="$(printf '%s' "$sms2_json" | jq -r '.data.dev_code')"
sms2_token="$(curl -fsS -X POST "$api_base/v1/auth/sms/verify" -H 'content-type: application/json' -d "{\"phone\":\"$phone\",\"code\":\"$dev_code2\"}" | jq -r '.data.token')"
sms2_me_id="$(curl -fsS "$api_base/v1/me" -H "Authorization: Bearer $sms2_token" | jq -r '.data.id')"
sms_me_id="$(curl -fsS "$api_base/v1/me" -H "Authorization: Bearer $sms_token" | jq -r '.data.id')"
test "$sms2_me_id" = "$sms_me_id"

# ---- 删除完整性：注销 = 删除账号；旧 token 立即失效（客户端 401 自动重登成新用户） ----
privacy_share_token="$(curl -fsS -X POST "$api_base/v1/shares" -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"source_type\":\"today\",\"source_id\":\"$today_plan_id\",\"include_photo\":false}" | jq -r '.data.token')"
curl -fsS -X DELETE "$api_base/v1/me/data" -H "Authorization: Bearer $token" -o /dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/today/plans/current" -H "Authorization: Bearer $token")" = "401"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/wardrobe/items" -H "Authorization: Bearer $token")" = "401"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/tasks/$analysis_task_id" -H "Authorization: Bearer $token")" = "401"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/me/profile" -H "Authorization: Bearer $token")" = "401"
# 公开分享 token 随账号删除失效
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/shares/$privacy_share_token")" = "404"
# 退出登录后旧 token 失效
curl -fsS -X DELETE "$api_base/v1/auth/session" -H "Authorization: Bearer $sms2_token" -o /dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/me" -H "Authorization: Bearer $sms2_token")" = "401"

printf 'e2e passed: unified tasks, idempotent PUT plans, plan regenerate, diagnostics, bootstrap, today, shares, wardrobe, grounded advisor, sms merge, logout and privacy deletion\n'
