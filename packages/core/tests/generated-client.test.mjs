import test from 'node:test'
import assert from 'node:assert/strict'
import { createGeneratedApiClient } from '../src/api/client.ts'

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
