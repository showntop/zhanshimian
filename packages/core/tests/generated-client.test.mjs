import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createGeneratedApiClient } from '../src/api/client.ts'

const openapi = await readFile(
  new URL('../../../contracts/openapi.yaml', import.meta.url),
  'utf8',
)
const packageJson = JSON.parse(
  await readFile(new URL('../package.json', import.meta.url), 'utf8'),
)

const requiredOperations = [
  ['POST', '/v1/media/upload-intents', 'createUploadIntent'],
  ['POST', '/v1/media/upload-intents/{id}/complete', 'completeUploadIntent'],
  ['POST', '/v1/media/demo', 'createDemoMedia'],
  ['POST', '/v1/assessments', 'createAssessment'],
  ['GET', '/v1/operations/{id}', 'getOperation'],
  ['GET', '/v1/operations', 'listOperations'],
  ['GET', '/v1/reports/current', 'getCurrentReport'],
  ['GET', '/v1/reports/{id}', 'getReport'],
  ['POST', '/v1/plan-sets', 'createPlanSet'],
  ['GET', '/v1/plan-sets/{id}', 'getPlanSet'],
  ['GET', '/v1/plan-sets', 'listPlanSets'],
  ['POST', '/v1/plan-variants/{id}/render-runs', 'createRenderRun'],
  ['GET', '/v1/render-runs/{id}', 'getRenderRun'],
  ['PUT', '/v1/plan-sets/{id}/selection', 'putPlanSetSelection'],
  ['POST', '/v1/selections/{id}/executions', 'createSelectionExecution'],
  ['GET', '/v1/executions/{id}', 'getExecution'],
  ['POST', '/v1/executions/{id}/events', 'createExecutionEvent'],
  ['POST', '/v1/generation-feedback', 'createGenerationFeedback'],
  ['POST', '/v1/execution-feedback', 'createExecutionFeedback'],
  ['GET', '/v1/home/bootstrap', 'getHomeBootstrap'],
]

function getPathBlock(path) {
  const marker = `  "${path}":`
  const start = openapi.indexOf(marker)
  assert.notEqual(start, -1, `missing OpenAPI path ${path}`)
  const next = openapi.indexOf('\n  "', start + marker.length)
  return openapi.slice(start, next === -1 ? undefined : next)
}

test('generated paths serialize path params and preserve the envelope', async () => {
  let request
  const client = createGeneratedApiClient({
    baseUrl: 'https://api.example',
    fetch: async (input) => {
      request = input
      return new Response(
        JSON.stringify({ data: { id: 'report-1', photo_set_id: 'photos-1' } }),
        {
          status: 200,
          headers: { 'content-type': 'application/json' },
        },
      )
    },
  })
  const result = await client.GET('/v1/reports/{id}', {
    params: { path: { id: 'report-1' } },
  })
  assert.equal(request.url, 'https://api.example/v1/reports/report-1')
  assert.equal(result.error, undefined)
  assert.equal(result.data.data.id, 'report-1')
})

test('frozen quality-loop operationIds keep their path and method mapping', () => {
  for (const [method, path, operationId] of requiredOperations) {
    assert.match(
      getPathBlock(path),
      new RegExp(`^    ${method.toLowerCase()}:\\n      operationId: ${operationId}$`, 'm'),
      `${operationId} must remain ${method} ${path}`,
    )
  }
})

test('legacy task and analysis paths stay absent', () => {
  assert.doesNotMatch(openapi, /^  "\/v1\/tasks(?:\/\{id\})?":/m)
  assert.doesNotMatch(openapi, /^  "\/v1\/analyses(?:\/\{id\})?":/m)
})

test('workspace scripts use local binaries and api:check includes lint', () => {
  assert.match(
    packageJson.scripts['api:generate'],
    /^pnpm exec openapi-typescript /,
  )
  assert.match(
    packageJson.scripts['api:check'],
    /node scripts\/check-openapi\.mjs/,
  )
  assert.match(
    packageJson.scripts['api:check'],
    /pnpm exec redocly lint \.\.\/\.\.\/contracts\/openapi\.yaml/,
  )
})
