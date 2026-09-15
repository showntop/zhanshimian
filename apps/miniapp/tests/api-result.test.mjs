import test from 'node:test'
import assert from 'node:assert/strict'
import { bodyOrThrow, dataOrThrow, isEmptySuccessStatus, noContentOrThrow, PublicApiError } from '../src/app/api/result.ts'

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

test('204/205 count as empty success bodies, everything else still parses', () => {
  // 「删除我的数据」成功就是 204 空体：中间件靠这个判定跳过 JSON 解析，
  // 把空体送进 JSON.parse('') 只会把一次成功的删除炸成 SyntaxError。
  assert.equal(isEmptySuccessStatus(204), true)
  assert.equal(isEmptySuccessStatus(205), true)
  assert.equal(isEmptySuccessStatus(200), false)
  assert.equal(isEmptySuccessStatus(404), false)
})

test('noContentOrThrow treats 204 without data as success, not as request_failed', () => {
  // openapi-fetch 对 204 给的是 { data: undefined }：走 bodyOrThrow 会把成功误判成
  // 'request_failed'——「删除我的数据」客户端 100% 报失败（数据其实已删）就是这么来的。
  assert.doesNotThrow(() => noContentOrThrow({ response: { status: 204 } }))
  assert.doesNotThrow(() => noContentOrThrow({ data: { ok: true }, response: { status: 200 } }))
  assert.throws(
    () =>
      noContentOrThrow({
        error: { error: { code: 'unauthorized', message: '登录已过期' } },
        response: { status: 401 },
      }),
    (error) => {
      assert.ok(error instanceof PublicApiError)
      assert.equal(error.code, 'unauthorized')
      assert.equal(error.statusCode, 401)
      return true
    },
  )
})
