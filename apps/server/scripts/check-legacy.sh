#!/bin/sh
# 遗留路径门禁：旧 Domain/Service/Repository/Provider 文件、旧符号与旧端点
# 一经删除即不许回流（Task 9 Step 1）。
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
repo="$(CDPATH= cd -- "$root/../.." && pwd)"

for path in \
  "$root/internal/domain/domain.go" \
  "$root/internal/repository/repository.go" \
  "$root/internal/service/service.go" \
  "$root/internal/provider/provider.go" \
  "$repo/packages/core/src/hooks/useTaskPolling.ts" \
  "$repo/apps/miniapp/src/hooks/use-stable-polling.ts"
do
  test ! -e "$path" || { echo "legacy path remains: $path" >&2; exit 1; }
done

if rg -n 'LatestTasksByRef|locked_at|generation_status|look_task|media_ids|generated_image_url|repository\.Repository|service\.ProviderOptions' \
  "$root/internal" "$repo/packages/core/src" "$repo/apps/miniapp/src"; then
  echo "legacy symbol remains" >&2
  exit 1
fi

if rg -n '/v1/tasks|/v1/analyses|/v1/plans' "$repo/contracts/openapi.yaml" "$repo/packages/core/src" "$repo/apps/miniapp/src"; then
  echo "legacy endpoint remains" >&2
  exit 1
fi

echo "legacy cutover gate passed"
