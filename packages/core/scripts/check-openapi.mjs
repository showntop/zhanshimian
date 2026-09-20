import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

// 必须在 @zsm/core 下执行：openapi-typescript 是它的依赖，pnpm 的严格
// node_modules 不会把 workspace 依赖挂到仓库根的 .bin 上（传 URL 对象当 cwd
// 时同理，表现为「openapi-typescript not found」，门禁静默失效）。
const core = fileURLToPath(new URL('../', import.meta.url))
const temp = mkdtempSync(join(tmpdir(), 'zsm-openapi-'))
const output = join(temp, 'schema.ts')

try {
  execFileSync(
    'pnpm',
    ['exec', 'openapi-typescript', '../../contracts/openapi.yaml', '-o', output],
    { cwd: core, stdio: 'inherit' },
  )
  const generated = readFileSync(output, 'utf8')
  const committed = readFileSync(
    new URL('../src/api/generated/schema.ts', import.meta.url),
    'utf8',
  )
  if (generated !== committed) {
    throw new Error(
      'OpenAPI generated schema drifted; run pnpm --filter @zsm/core api:generate',
    )
  }
} finally {
  rmSync(temp, { recursive: true, force: true })
}
