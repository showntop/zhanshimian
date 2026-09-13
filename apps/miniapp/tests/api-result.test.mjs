import test from 'node:test'
import assert from 'node:assert/strict'
import { bodyOrThrow, dataOrThrow, PublicApiError } from '../src/app/api/result.ts'

test('dataOrThrow unwraps the envelope', () => {
  const data = dataOrThrow({
    data: { data: { id: 'report-1' } },
    response: { status: 200 },
  })
  assert.deepEqual(data, { id: 'report-1' })
})

test('bodyOrThrow returns the whole body when no envelope is used', () => {
  const body = bodyOrThrow({
    data: { operation: { id: 'op-1', status: 'accepted' } },
    response: { status: 201 },
  })
  assert.deepEqual(body, { operation: { id: 'op-1', status: 'accepted' } })
})

test('dataOrThrow preserves public request id and retryability', () => {
  assert.throws(
    () =>
      dataOrThrow({
        error: {
          error: {
            code: 'render_failed',
            message: '这一套暂时没有生成成功',
            request_id: 'req-1',
            retryable: true,
          },
        },
        response: { status: 422 },
      }),
    (error) => {
      assert.ok(error instanceof PublicApiError)
      assert.equal(error.code, 'render_failed')
      assert.equal(error.requestId, 'req-1')
      assert.equal(error.retryable, true)
      assert.equal(error.statusCode, 422)
      assert.equal(error.message, '这一套暂时没有生成成功')
      return true
    },
  )
})

test('dataOrThrow falls back to a retryable-free public message', () => {
  assert.throws(
    () => dataOrThrow({ error: {}, response: { status: 500 } }),
    (error) => {
      assert.ok(error instanceof PublicApiError)
      assert.equal(error.code, 'request_failed')
      assert.equal(error.message, '请求没有成功，请重试')
      assert.equal(error.requestId, '')
      assert.equal(error.retryable, false)
      assert.equal(error.statusCode, 500)
      return true
    },
  )
})

test('a thrown PublicApiError exposes only public fields', () => {
  const error = new PublicApiError('render_failed', '失败', 422, 'req-1', true)
  assert.ok(error instanceof Error)
  assert.equal(error.name, 'PublicApiError')
  assert.equal(error.message, '失败')
  // 内部字段绝不能顺着错误对象漏到界面上：供应商、模型和链路追踪都只留在服务端。
  for (const leaked of ['vendor', 'model', 'trace_id', 'provider_key', 'stack_detail']) {
    assert.equal(leaked in error, false, `${leaked} leaked onto the public error`)
  }
})
