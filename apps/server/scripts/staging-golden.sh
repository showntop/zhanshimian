#!/bin/sh
# staging-golden.sh — 锁定金集发布门禁。UPLOOK_GOLDEN_DATASET_DIR 指向私有
# 对象存储挂载的授权金集;任一 hard gate 不满足即非零退出。
set -eu

go run ./cmd/eval \
  --manifest eval/goldens/manifest.json \
  --dataset-split locked \
  --dataset-root "${UPLOOK_GOLDEN_DATASET_DIR:?UPLOOK_GOLDEN_DATASET_DIR is required}" \
  --routing-config config/ai-routing.production.json \
  --output /tmp/uplook-locked-release.json

jq -e '
  .photo_role_accuracy == 1 and
  .source_mismatch_count == 0 and
  .sensitive_inference_count == 0 and
  .finding_evidence_completeness == 1 and
  .finding_evidence_support_rate >= 0.95 and
  .plan_grounding_completeness == 1 and
  .plan_pair_difference_pass_rate == 1 and
  .single_image_fallback_count == 0 and
  .severe_bad_render_publish_rate < 0.01 and
  .identity_human_pass_rate >= 0.90 and
  .render_spec_match_rate >= 0.85 and
  .source_operation_invocation_feedback_link_rate == 1 and
  .webp_publish_count == 0 and
  .photo_check_p95_ms <= 12000 and
  .report_p95_ms <= 90000 and
  .plan_text_p95_ms <= 60000 and
  .first_render_p95_ms <= 120000
' /tmp/uplook-locked-release.json >/dev/null
