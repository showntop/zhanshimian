#!/bin/sh
# 质量核心切换后的端到端回归(Stage 8 Task 12):
#   真实 upload intent 三图 → assessment → operation → report → plan set →
#   render publication → selection → execution 事件流 → 两类 feedback,
#   再经 Reader 验证全部外围模块(Today/Advisor/Hair/Outfit/Purchase/Share/Home)
#   与回归面(登录、支付签名、天气降级、COS 签名 URL、越权 404、隐私删除)。
# 通过时最后一行输出: e2e passed: quality core and peripherals cut over
set -eu

api_base="${API_BASE_URL:-http://127.0.0.1:58000}"
server_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
demo_dir="$server_root/assets/demo"
run_tag="e2e-$(date +%s)-$$"

# 每次调用生成全新幂等键:计数器在 $(...) 子壳里不持久,会导致跨调用重键 409。
idem_key() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr 'A-Z' 'a-z'
  elif [ -r /proc/sys/kernel/random/uuid ]; then
    cat /proc/sys/kernel/random/uuid
  else
    printf '%s-%s-%s\n' "$run_tag" "$(date +%s)" "$$"
  fi
}

now_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# 轮询 Operation 直到终态;succeeded 时 echo 完整 payload,其它终态写 stderr 并失败。
wait_operation() {
  operation_id="$1"
  # 第二参数为预算秒数,默认 180(计划冻结值)。plan_set 最坏路径 =
  # 90s 模型超时 + 一次基础设施重试 + 一次内容重试 ≈ 4.5 分钟(第 7 轮实测),
  # 调用点显式放大预算;其余操作保持 180。
  op_budget="${2:-180}"
  attempt=0
  while [ "$attempt" -lt "$op_budget" ]; do
    payload="$(curl -fsS "$api_base/v1/operations/$operation_id" -H "Authorization: Bearer $token")"
    status="$(printf '%s' "$payload" | jq -r '.data.status')"
    case "$status" in
      succeeded) printf '%s' "$payload"; return 0 ;;
      failed|cancelled|superseded) printf '%s\n' "$payload" >&2; return 1 ;;
    esac
    attempt=$((attempt + 1))
    sleep 1
  done
  echo "operation timed out: $operation_id" >&2
  return 1
}

# 真实上传:upload intent → 按授权头 PUT 字节到 COS → complete;echo ready 资产 id。
# 注意:本函数在 $(...) 子壳里运行,bash 不在子壳继承 set -e,必须显式 || return 1。
upload_asset() {
  up_purpose="$1"; up_file="$2"
  up_size=$(stat -f%z "$up_file" 2>/dev/null || stat -c%s "$up_file")
  up_sha=$(shasum -a 256 "$up_file" | cut -d' ' -f1)
  up_intent="$(curl -fsS -X POST "$api_base/v1/media/upload-intents" \
    -H "Authorization: Bearer $token" -H 'content-type: application/json' \
    -H "Idempotency-Key: $(idem_key)" \
    -d "{\"purpose\":\"$up_purpose\",\"mime_type\":\"image/jpeg\",\"byte_size\":$up_size,\"sha256\":\"$up_sha\"}")" || return 1
  printf '%s' "$up_intent" | jq -e '.data.id != null and .data.upload.url != null' >/dev/null || return 1
  up_url="$(printf '%s' "$up_intent" | jq -r '.data.upload.url')"
  up_intent_id="$(printf '%s' "$up_intent" | jq -r '.data.id')"
  set --
  while IFS= read -r up_header; do
    set -- "$@" -H "$up_header"
  done <<EOF_HEADERS
$(printf '%s' "$up_intent" | jq -r '.data.upload.headers | to_entries[] | "\(.key): \(.value)"')
EOF_HEADERS
  curl -fsS -X PUT "$up_url" "$@" --data-binary @"$up_file" -o /dev/null || return 1
  up_completed="$(curl -fsS -X POST "$api_base/v1/media/upload-intents/$up_intent_id/complete" \
    -H "Authorization: Bearer $token" -H "Idempotency-Key: $(idem_key)")" || return 1
  printf '%s' "$up_completed" | jq -e '.data.state == "ready"' >/dev/null || return 1
  printf '%s' "$up_completed" | jq -r '.data.id'
}

# 追加一条 Execution 事件(CAS):$1 execution id,$2 期望版本,$3 类型,$4 step id(可空);
# echo 事件响应(调用方从新 version 继续)。
append_event() {
  ev_exec="$1"; ev_version="$2"; ev_type="$3"; ev_step="${4:-}"
  if [ -n "$ev_step" ]; then
    ev_body="{\"client_event_id\":\"$(idem_key)\",\"type\":\"$ev_type\",\"step_id\":\"$ev_step\",\"occurred_at\":\"$(now_utc)\"}"
  else
    ev_body="{\"client_event_id\":\"$(idem_key)\",\"type\":\"$ev_type\",\"occurred_at\":\"$(now_utc)\"}"
  fi
  curl -fsS -X POST "$api_base/v1/executions/$ev_exec/events" \
    -H "Authorization: Bearer $token" -H 'content-type: application/json' \
    -H "Idempotency-Key: $(idem_key)" -H "If-Match: \"$ev_version\"" \
    -d "$ev_body" || return 1
}

login_json="$(curl -fsS -X POST "$api_base/v1/auth/dev" -H 'content-type: application/json' -d '{"nickname":"e2e 主用户"}')"
token="$(printf '%s' "$login_json" | jq -r '.data.token')"
other_token="$(curl -fsS -X POST "$api_base/v1/auth/dev" -H 'content-type: application/json' -d '{"nickname":"e2e 越权用户"}' | jq -r '.data.token')"

# ---- 1. 三图 upload intent complete → assessment → operation 成功并返回 Report ----
face_asset_id="$(upload_asset face "$demo_dir/face.jpg")"
side_asset_id="$(upload_asset side "$demo_dir/side.jpg")"
body_asset_id="$(upload_asset body "$demo_dir/body.jpg")"

assessment_json="$(curl -fsS -X POST "$api_base/v1/assessments" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $(idem_key)" \
  -d "{\"photos\":{\"face_asset_id\":\"$face_asset_id\",\"side_asset_id\":\"$side_asset_id\",\"body_asset_id\":\"$body_asset_id\"}}")"
assessment_op_id="$(printf '%s' "$assessment_json" | jq -r '.operation.id')"
assessment_op="$(wait_operation "$assessment_op_id")"
printf '%s' "$assessment_op" | jq -e '.data.result_type == "report" and (.data.result_id | length) > 0' >/dev/null
report_id="$(printf '%s' "$assessment_op" | jq -r '.data.result_id')"
report_json="$(curl -fsS "$api_base/v1/reports/$report_id" -H "Authorization: Bearer $token")"
printf '%s' "$report_json" | jq -e ".data.id == \"$report_id\"" >/dev/null

# ---- 2. Report 每条 finding 有来源照片、anchor 与可见观察 ----
printf '%s' "$report_json" | jq -e '
  (.data.findings | length) >= 1 and
  (.data.findings | all(
    (.source_photo.item_id | type == "string" and length > 0) and
    (.source_photo.role | type == "string" and length > 0) and
    (.anchor | type == "object") and
    (.visible_observation | type == "string" and length > 0)
  ))' >/dev/null
# COS 签名 URL 回归:报告源照片读路径必须给即时签名 URL,不给裸 object key。
printf '%s' "$report_json" | jq -e '
  [.data.source_media[].media.url] | all(type == "string" and contains("q-sign-algorithm="))' >/dev/null

# ---- 3. PlanSet:恰好三套、恰好一套 recommended、每步有 grounding、任意两套至少两个差异 ----
planset_post="$(curl -fsS -X POST "$api_base/v1/plan-sets" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $(idem_key)" \
  -d "{\"report_id\":\"$report_id\",\"scene\":\"general\",\"brief\":{\"focus\":\"balanced\",\"preparation\":\"closet\",\"impression\":\"natural\"}}")"
planset_id="$(printf '%s' "$planset_post" | jq -r '.data.id')"
planset_op_id="$(printf '%s' "$planset_post" | jq -r '.operation.id // empty')"
if [ -n "$planset_op_id" ]; then
  wait_operation "$planset_op_id" 420 >/dev/null
fi
planset_json="$(curl -fsS "$api_base/v1/plan-sets/$planset_id" -H "Authorization: Bearer $token")"
printf '%s' "$planset_json" | jq -e '
  (.data.variants | length) == 3 and
  ([.data.variants[] | select(.recommended)] | length) == 1 and
  ([.data.variants[].steps[] | select((.groundings | length) == 0)] | length) == 0 and
  ([.data.variants[].difference_tags | length] | min) >= 2 and
  ([.data.variants[].difference_tags] as $tags |
    [range(0; $tags | length) as $i | range($i + 1; $tags | length) as $j |
      (($tags[$i] - $tags[$j]) + ($tags[$j] - $tags[$i])) | length] | min) >= 2
' >/dev/null
variant_id="$(printf '%s' "$planset_json" | jq -r '[.data.variants[] | select(.recommended)][0].id')"

# ---- 4. Render:只返回 published JPEG,generated_preview,无 Task/Provider/内部分数 ----
render_post="$(curl -fsS -X POST "$api_base/v1/plan-variants/$variant_id/render-runs" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $(idem_key)" -d '{}')"
render_run_id="$(printf '%s' "$render_post" | jq -r '.data.id')"
render_op_id="$(printf '%s' "$render_post" | jq -r '.operation.id // .data.render.operation_id')"
wait_operation "$render_op_id" >/dev/null
render_json="$(curl -fsS "$api_base/v1/render-runs/$render_run_id" -H "Authorization: Bearer $token")"
printf '%s' "$render_json" | jq -e '
  .data.render.state == "ready" and
  .data.render.media != null and
  .data.render.media.mime_type == "image/jpeg" and
  .data.render.media.source_kind == "generated_preview" and
  .data.render.media.display_label == "风格参考" and
  (.data.render.media.url | contains("q-sign-algorithm=")) and
  (.data.render | has("score") | not) and
  (.data.render | has("task") | not) and
  (.data.render | has("provider") | not)' >/dev/null
# 泄露面:渲染响应全文不得出现厂商/模型/任务字样。
if printf '%s' "$render_json" | grep -Eiq 'qwen|wanx|dashscope|volcengine|seedream|provider|task_id|internal_score'; then
  echo "render response leaks provider/task/score internals" >&2; exit 1
fi
publication_id="$(printf '%s' "$render_json" | jq -r '.data.render.publication_id')"
render_asset_id="$(printf '%s' "$render_json" | jq -r '.data.render.media.asset_id')"
# published JPEG 本体可经签名 URL 下载且确为 JPEG。
render_media_url="$(printf '%s' "$render_json" | jq -r '.data.render.media.url')"
downloaded_jpeg="$demo_dir/.e2e-downloaded-$$.jpg"
curl -fsS "$render_media_url" -o "$downloaded_jpeg"
test "$(od -An -tx1 -N2 "$downloaded_jpeg" | tr -d ' \n')" = "ffd8"
rm -f "$downloaded_jpeg"

# ---- 5. Home 返回契约键(report/plan_set/today_plan),不返回 active_tasks ----
home_json="$(curl -fsS "$api_base/v1/home/bootstrap" -H "Authorization: Bearer $token")"
printf '%s' "$home_json" | jq -e '
  (.data.active_operations | type == "array") and
  (.data | has("active_tasks") | not) and
  (.data | has("current_report") | not) and
  (.data.report.id == "'"$report_id"'") and
  (.data.report.source_media.face.media.url | length) > 0 and
  (.data.plan_set.id == "'"$planset_id"'") and
  (.data.plan_set.variants | length) == 3 and
  (.data.billing.credits | type == "number")' >/dev/null

# ---- 6. 外围 Reader:不携带任何本地恢复 ID 也能读取当前 grounding ----
# 发型目录表未进 baseline(交接跟进项),adapter 对缺表降级为空目录;
# 这里断言 Reader 面无 ID 可读且返回数组,目录非空由 catalog 进 baseline 后恢复断言。
curl -fsS "$api_base/v1/hairstyles" -H "Authorization: Bearer $token" \
  | jq -e '.data | type == "array"' >/dev/null
today_context="$(curl -fsS "$api_base/v1/today/context?city=%E6%9D%AD%E5%B7%9E&schedule=%E9%80%9A%E5%8B%A4" -H "Authorization: Bearer $token")"
# 天气失败降级面:上下文端点不依赖天气成功,date/day_type 恒在(失败回退由 service 测试覆盖)。
printf '%s' "$today_context" | jq -e '
  .data.city == "杭州" and (.data.date | length) > 0 and (.data.day_type | length) > 0' >/dev/null
today_plan="$(curl -fsS -X POST "$api_base/v1/today/plans" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d '{"city":"杭州","schedule":"通勤"}')"
printf '%s' "$today_plan" | jq -e '.data.id != null and (.data.steps | length) >= 1' >/dev/null
today_plan_id="$(printf '%s' "$today_plan" | jq -r '.data.id')"
# 契约:无生效方案时 /current 回 200 data:null(越权用户无任何方案);创建不自动生效,
# 必须显式 activate(互斥)后 /current 才返回同一方案。
curl -fsS "$api_base/v1/today/plans/current" -H "Authorization: Bearer $other_token" \
  | jq -e '.data == null' >/dev/null
curl -fsS -X POST "$api_base/v1/today/plans/$today_plan_id/activate" \
  -H "Authorization: Bearer $token" -H "Idempotency-Key: $(idem_key)" >/dev/null
curl -fsS "$api_base/v1/today/plans/current" -H "Authorization: Bearer $token" \
  | jq -e ".data.id == \"$today_plan_id\" and .data.active == true" >/dev/null
advisor_json="$(curl -fsS -X POST "$api_base/v1/advisor/messages" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d '{"content":"今天这套还想更利落一点"}')"
printf '%s' "$advisor_json" | jq -e '.data.content | length > 0' >/dev/null
outfit_demo="$(curl -fsS -X POST "$api_base/v1/media/demo" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"kind":"outfit"}')"
# 契约:createDemoMedia 返回 DisplayMedia(demo_example/效果示例 + 签名 URL)。
printf '%s' "$outfit_demo" | jq -e '
  (.data.asset_id | type == "string" and length > 0) and
  (.data.url | type == "string" and length > 0) and
  (.data.url_expires_at | type == "string" and length > 0) and
  .data.source_kind == "demo_example" and
  .data.display_label == "效果示例" and
  (.data | has("id") | not)' >/dev/null
outfit_media_id="$(printf '%s' "$outfit_demo" | jq -r '.data.asset_id')"
outfit_json="$(curl -fsS -X POST "$api_base/v1/diagnostics" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"kind\":\"outfit\",\"media_id\":\"$outfit_media_id\",\"scene\":\"daily\"}")"
printf '%s' "$outfit_json" | jq -e '.data.findings | length >= 1' >/dev/null
outfit_diagnostic_id="$(printf '%s' "$outfit_json" | jq -r '.data.id')"
product_media_id="$(curl -fsS -X POST "$api_base/v1/media/demo" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"kind":"product"}' | jq -r '.data.asset_id')"
curl -fsS -X POST "$api_base/v1/diagnostics" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"kind\":\"purchase\",\"media_id\":\"$product_media_id\",\"scene\":\"daily\"}" \
  | jq -e '.data.conclusion | length > 0' >/dev/null

# ---- 7. Share:快照只来自 published asset,撤销后 404 ----
share_json="$(curl -fsS -X POST "$api_base/v1/shares" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"source_type\":\"plan_variant\",\"source_id\":\"$variant_id\",\"include_photo\":true}")"
printf '%s' "$share_json" | jq -e "
  .data.snapshot.asset_id == \"$render_asset_id\" and
  .data.snapshot.source_kind == \"generated_preview\" and
  .data.snapshot.display_label == \"风格参考\"" >/dev/null
share_id="$(printf '%s' "$share_json" | jq -r '.data.id')"
share_token="$(printf '%s' "$share_json" | jq -r '.data.token')"
curl -fsS "$api_base/v1/shares/$share_token" \
  | jq -e '.data.media.url | contains("q-sign-algorithm=")' >/dev/null
curl -fsS -X DELETE "$api_base/v1/shares/$share_id" -H "Authorization: Bearer $token" >/dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/shares/$share_token")" = "404"

# ---- 8. 越权读取所有外围资源统一 404 ----
for url in \
  "reports/$report_id" \
  "plan-sets/$planset_id" \
  "render-runs/$render_run_id" \
  "diagnostics/$outfit_diagnostic_id" \
  "operations/$assessment_op_id"
do
  test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/$url" -H "Authorization: Bearer $other_token")" = "404"
done
# 越权建分享/执行同样 404:他人 plan_variant 不可作为来源。
# 注意:JSON 必须先赋给变量再引用 —— 把带逗号的 "{...}" 字面量直接写进
# test "$(...)" 命令行会触发 bash 花括号展开,curl 被复制成多次执行(第 15
# 轮实跑死于 test: too many arguments);赋值语句语境不展开。
other_share_payload="$(printf '{"source_type":"plan_variant","source_id":"%s","include_photo":true}' "$variant_id")"
test "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$api_base/v1/shares" \
  -H "Authorization: Bearer $other_token" -H 'content-type: application/json' \
  -d "$other_share_payload")" = "404"

# ---- 9. Selection → Execution 快照 → 事件 CAS → 两类 Feedback 关联完整 ----
selection_json="$(curl -fsS -X PUT "$api_base/v1/plan-sets/$planset_id/selection" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $(idem_key)" \
  -d "{\"plan_variant_id\":\"$variant_id\",\"render_publication_id\":\"$publication_id\"}")"
printf '%s' "$selection_json" | jq -e "
  .data.plan_variant_id == \"$variant_id\" and
  .data.render_publication_id == \"$publication_id\"" >/dev/null
selection_id="$(printf '%s' "$selection_json" | jq -r '.data.id')"
# 越权读取他人 selection 的执行入口统一 404(放在拿到 execution id 后一并校验)。

execution_json="$(curl -fsS -X POST "$api_base/v1/selections/$selection_id/executions" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $(idem_key)" -d '{}')"
printf '%s' "$execution_json" | jq -e '.data.version == 1 and (.data.steps | length) >= 1' >/dev/null
execution_id="$(printf '%s' "$execution_json" | jq -r '.data.id')"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/executions/$execution_id" -H "Authorization: Bearer $other_token")" = "404"

exec_version=1
event_resp="$(append_event "$execution_id" "$exec_version" started)"
exec_version="$(printf '%s' "$event_resp" | jq -r '.data.execution.version')"
for step_id in $(printf '%s' "$execution_json" | jq -r '.data.steps[].id'); do
  event_resp="$(append_event "$execution_id" "$exec_version" step_completed "$step_id")"
  exec_version="$(printf '%s' "$event_resp" | jq -r '.data.execution.version')"
done
event_resp="$(append_event "$execution_id" "$exec_version" completed)"
printf '%s' "$event_resp" | jq -e '.data.execution.state == "completed"' >/dev/null
# 过期版本的 CAS 写入必须被 412 拒绝。
stale_event_payload="$(printf '{"client_event_id":"%s","type":"abandoned","occurred_at":"%s"}' "$(idem_key)" "$(now_utc)")"
test "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$api_base/v1/executions/$execution_id/events" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $(idem_key)" -H 'If-Match: "1"' \
  -d "$stale_event_payload")" = "412"

generation_feedback="$(curl -fsS -X POST "$api_base/v1/generation-feedback" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $(idem_key)" \
  -d "{\"publication_id\":\"$publication_id\",\"tags\":[\"unnatural\"]}")"
printf '%s' "$generation_feedback" | jq -e "
  .data.publication_id == \"$publication_id\" and
  .data.render_run_id == \"$render_run_id\" and
  .data.acknowledgement_code == \"feedback_recorded\"" >/dev/null

exec_feedback_body="{\"execution_id\":\"$execution_id\",\"tags\":[\"too_formal\"],\"preference\":{\"kind\":\"less_formal\",\"category\":\"overall\"}}"
exec_feedback_key="$(idem_key)"
execution_feedback="$(curl -fsS -X POST "$api_base/v1/execution-feedback" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $exec_feedback_key" -d "$exec_feedback_body")"
printf '%s' "$execution_feedback" | jq -e "
  .data.execution_id == \"$execution_id\" and
  .data.selection_id == \"$selection_id\" and
  .data.plan_set_id == \"$planset_id\" and
  .data.acknowledgement_code == \"less_formal_saved\" and
  (.data.applied_memories | length) == 1" >/dev/null
# 同一幂等键重放拿回同一条(200 而非新建)。
curl -fsS -X POST "$api_base/v1/execution-feedback" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H "Idempotency-Key: $exec_feedback_key" -d "$exec_feedback_body" \
  | jq -e ".data.execution_id == \"$execution_id\"" >/dev/null

# ---- 10. 回归面:登录、支付签名、天气降级(见上)、COS 签名 URL(见上) ----
# sms 登录:唯一手机号避开合法限流;同号二次登录数据互通;登出后旧 token 失效。
phone="138$(printf '%08d' $(( $(date +%s) % 100000000 )))"
sms_json="$(curl -fsS -X POST "$api_base/v1/auth/sms/request" -H 'content-type: application/json' -d "{\"phone\":\"$phone\"}")"
dev_code="$(printf '%s' "$sms_json" | jq -r '.data.dev_code')"
test -n "$dev_code"
sms_token="$(curl -fsS -X POST "$api_base/v1/auth/sms/verify" -H 'content-type: application/json' \
  -d "{\"phone\":\"$phone\",\"code\":\"$dev_code\"}" | jq -r '.data.token')"
test "$(curl -fsS "$api_base/v1/me" -H "Authorization: Bearer $sms_token" | jq -r '.data.identities | length')" -ge 1
dev_code2="$(curl -fsS -X POST "$api_base/v1/auth/sms/request" -H 'content-type: application/json' -d "{\"phone\":\"$phone\"}" | jq -r '.data.dev_code')"
sms2_token="$(curl -fsS -X POST "$api_base/v1/auth/sms/verify" -H 'content-type: application/json' \
  -d "{\"phone\":\"$phone\",\"code\":\"$dev_code2\"}" | jq -r '.data.token')"
test "$(curl -fsS "$api_base/v1/me" -H "Authorization: Bearer $sms2_token" | jq -r '.data.id')" = \
     "$(curl -fsS "$api_base/v1/me" -H "Authorization: Bearer $sms_token" | jq -r '.data.id')"
curl -fsS -X DELETE "$api_base/v1/auth/session" -H "Authorization: Bearer $sms2_token" -o /dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/me" -H "Authorization: Bearer $sms2_token")" = "401"

# 支付签名回归:dev 未配置微信支付时,下单必须失败关闭为类型化 payment_unavailable,
# 而不是 500 或静默放行(签名路径本身由 billing 单元/集成测试覆盖)。
billing_status="$(curl -sS -o "$demo_dir/.e2e-billing-$$.json" -w '%{http_code}' -X POST "$api_base/v1/billing/orders" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' -d '{"sku_id":"pack_3"}')"
test "$billing_status" = "503"
jq -e '.error.code == "payment_unavailable"' "$demo_dir/.e2e-billing-$$.json" >/dev/null
rm -f "$demo_dir/.e2e-billing-$$.json"
curl -fsS "$api_base/v1/billing/me" -H "Authorization: Bearer $token" >/dev/null

# ---- 收尾:隐私删除回归(注销 = 删除账号;旧 token 与公开分享立即失效) ----
privacy_share_token="$(curl -fsS -X POST "$api_base/v1/shares" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -d "{\"source_type\":\"plan_variant\",\"source_id\":\"$variant_id\",\"include_photo\":true}" | jq -r '.data.token')"
curl -fsS -X DELETE "$api_base/v1/me/data" -H "Authorization: Bearer $token" -o /dev/null
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/reports/$report_id" -H "Authorization: Bearer $token")" = "401"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/home/bootstrap" -H "Authorization: Bearer $token")" = "401"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/operations/$assessment_op_id" -H "Authorization: Bearer $token")" = "401"
test "$(curl -sS -o /dev/null -w '%{http_code}' "$api_base/v1/shares/$privacy_share_token")" = "404"

printf 'e2e passed: quality core and peripherals cut over\n'
