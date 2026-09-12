import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const root = new URL('../../../', import.meta.url)
const temp = mkdtempSync(join(tmpdir(), 'zsm-openapi-'))
const output = join(temp, 'schema.ts')

try {
  execFileSync(
    'pnpm',
    ['exec', 'openapi-typescript', 'contracts/openapi.yaml', '-o', output],
    { cwd: root, stdio: 'inherit' },
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
