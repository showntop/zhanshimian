#!/usr/bin/env node

import { existsSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..')
const openapiPath = join(repoRoot, 'contracts', 'openapi.yaml')
const generatedPath = join(
  repoRoot,
  'packages',
  'core',
  'src',
  'api',
  'generated',
  'schema.ts',
)

const requiredOperationIds = new Set([
  'createUploadIntent',
  'completeUploadIntent',
  'createDemoMedia',
  'createAssessment',
  'getOperation',
  'listOperations',
  'getCurrentReport',
  'getReport',
  'createPlanSet',
  'getPlanSet',
  'listPlanSets',
  'createRenderRun',
  'getRenderRun',
  'putPlanSetSelection',
  'createSelectionExecution',
  'getExecution',
  'createExecutionEvent',
  'createGenerationFeedback',
  'createExecutionFeedback',
  'getHomeBootstrap',
])

function collectMatches(source, pattern) {
  const values = []
  for (const match of source.matchAll(pattern)) values.push(match[1])
  return values
}

function unique(values, label) {
  const result = new Set()
  for (const value of values) {
    if (result.has(value)) throw new Error(`${label} 重复: ${value}`)
    result.add(value)
  }
  return result
}

function difference(left, right) {
  return [...left].filter((value) => !right.has(value)).sort()
}

function collectSpecPaths(source) {
  const paths = []
  const pattern = /^ {2}(?:"([^"]+)"|'([^']+)'|([^\s:]+)):\s*$/gm
  for (const match of source.matchAll(pattern)) {
    const path = match[1] ?? match[2] ?? match[3]
    if (path.startsWith('/')) paths.push(path)
  }
  return paths
}

function main() {
  if (!existsSync(openapiPath) || !existsSync(generatedPath)) {
    throw new Error('OpenAPI 或 generated schema 不存在；请先运行 api:generate')
  }

  const openapi = readFileSync(openapiPath, 'utf8')
  const generated = readFileSync(generatedPath, 'utf8')
  const specOperations = unique(
    collectMatches(openapi, /^\s+operationId:\s*([A-Za-z][A-Za-z0-9]*)\s*$/gm),
    'OpenAPI operationId',
  )
  const specPaths = unique(collectSpecPaths(openapi), 'OpenAPI path')
  const generatedOperationsBlock =
    /export interface operations \{([\s\S]*?)^\}/m.exec(generated)?.[1]
  if (!generatedOperationsBlock) {
    throw new Error('generated schema 中缺少 operations interface')
  }
  const generatedOperations = unique(
    collectMatches(generatedOperationsBlock, /^\s{4}([A-Za-z][A-Za-z0-9]*):\s*\{/gm),
    'generated operationId',
  )

  const missingRequired = difference(requiredOperationIds, specOperations)
  const missingGenerated = difference(specOperations, generatedOperations)
  const extraGenerated = difference(generatedOperations, specOperations)
  if (missingRequired.length || missingGenerated.length || extraGenerated.length) {
    if (missingRequired.length) {
      console.error(`[check-sync] 缺少锁定 operationId: ${missingRequired.join(', ')}`)
    }
    if (missingGenerated.length) {
      console.error(`[check-sync] generated 缺少: ${missingGenerated.join(', ')}`)
    }
    if (extraGenerated.length) {
      console.error(`[check-sync] generated 多出: ${extraGenerated.join(', ')}`)
    }
    process.exit(1)
  }

  const legacyPaths = [...specPaths].filter(
    (path) =>
      path === '/v1/tasks' ||
      path.startsWith('/v1/tasks/') ||
      path === '/v1/analyses' ||
      path.startsWith('/v1/analyses/') ||
      path === '/v1/reports/{id}/plans' ||
      path === '/v1/plans' ||
      path.startsWith('/v1/plans/'),
  )
  if (legacyPaths.length) {
    throw new Error(`仍存在旧公开路径: ${legacyPaths.sort().join(', ')}`)
  }
  if (existsSync(join(repoRoot, 'packages', 'core', 'src', 'api', 'generated.ts'))) {
    throw new Error('禁止存在 packages/core/src/api/generated.ts')
  }

  console.log(
    `[check-sync] ✅ 契约与 generated client 完全一致（${specOperations.size} 个 operationId）`,
  )
}

main()
