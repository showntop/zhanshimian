// createApiClient 测试：401 单飞重登重放一次、信封解析、错误映射、上传重试
import test from 'node:test'
import assert from 'node:assert/strict'
import { createApiClient, ApiError } from '../src/http/client.ts'
import { localizeDevImages, rewriteLoopbackAssetURLs } from '../src/http/images.ts'
import { createApiEndpoints } from '../src/api/endpoints.ts'

function memoryStore(initial = '') {
  let token = initial
  return {
    get: () => token,
    set: (next) => {
      token = next
    },
    clear: () => {
      token = ''
    }
  }
}

function fakeAdapter(handler) {
  const calls = []
  return {
    calls,
    adapter: {
      async request(req) {
        calls.push(req)
        return handler(req, calls.length)
      }
    }
  }
}

test('2xx 解 {data} 信封返回 data；task 引用随 envelope 暴露', async () => {
  const { adapter } = fakeAdapter(() => ({ statusCode: 200, data: { data: { id: 'r1' }, task: { id: 't1', type: 'analysis' } } }))
  const client = createApiClient({ adapter, baseUrl: 'https://api.test' })
  assert.deepEqual(await client.request('/v1/reports/r1'), { id: 'r1' })
  const envelope = await client.requestEnvelope('/v1/reports/r1')
  assert.equal(envelope.task.id, 't1')
})

test('非 2xx 抛 ApiError{code,message,statusCode,requestId}', async () => {
  const { adapter } = fakeAdapter(() => ({
    statusCode: 422,
    data: { error: { code: 'invalid_media', message: '照片不满足要求', request_id: 'req-1' } }
  }))
  const client = createApiClient({ adapter, baseUrl: 'https://api.test' })
  await assert.rejects(
    () => client.request('/v1/analyses', { method: 'POST', data: {} }),
    (error) => {
      assert.ok(error instanceof ApiError)
      assert.equal(error.code, 'invalid_media')
      assert.equal(error.statusCode, 422)
      assert.equal(error.requestId, 'req-1')
      assert.equal(error.message, '照片不满足要求')
      return true
    }
  )
})

test('401 单飞：并发 3 个 401 只 relogin 一次，全部重放成功', async () => {
  let reloginCalls = 0
  let tokensIssued = 0
  const { adapter, calls } = fakeAdapter((req) => {
    if (req.path === '/v1/auth/wechat') {
      reloginCalls += 1
      return { statusCode: 200, data: { data: { token: `t${++tokensIssued}` } } }
    }
    const auth = req.header?.Authorization ?? ''
    // 重登后签发的 token 形如 t1/t2…（非 'Bearer stale' 即有效）
    return auth.startsWith('Bearer t')
      ? { statusCode: 200, data: { data: { ok: true } } }
      : { statusCode: 401, data: { error: { code: 'unauthorized', message: '登录过期' } } }
  })
  const store = memoryStore('stale')
  const client = createApiClient({
    adapter,
    baseUrl: 'https://api.test',
    store,
    relogin: async () => {
      const session = await client.request('/v1/auth/wechat', { method: 'POST', data: { code: 'c' } })
      store.set(session.token)
    }
  })

  const results = await Promise.all([
    client.request('/v1/home/bootstrap'),
    client.request('/v1/home/bootstrap'),
    client.request('/v1/today/plans/current')
  ])
  assert.equal(reloginCalls, 1)
  assert.deepEqual(results, [{ ok: true }, { ok: true }, { ok: true }])
  // 重放后使用新 token
  assert.equal(store.get(), 't1')
  const replayed = calls.filter((c) => c.path === '/v1/home/bootstrap')
  assert.equal(replayed.length, 4) // 2 次初始 401 + 2 次重放（today 路径另计）
})

test('401 只重放一次：重放仍 401 则抛错不无限循环', async () => {
  const { adapter } = fakeAdapter(() => ({ statusCode: 401, data: { error: { code: 'unauthorized', message: 'x' } } }))
  let relogins = 0
  const client = createApiClient({
    adapter,
    baseUrl: 'https://api.test',
    store: memoryStore(),
    relogin: async () => {
      relogins += 1
    }
  })
  await assert.rejects(() => client.request('/v1/plans/1'), (error) => error.statusCode === 401)
  assert.equal(relogins, 1)
})

test('/v1/auth/ 路径 401 不触发重登（避免循环）', async () => {
  let relogins = 0
  const { adapter } = fakeAdapter(() => ({ statusCode: 401, data: { error: { code: 'unauthorized', message: 'x' } } }))
  const client = createApiClient({
    adapter,
    baseUrl: 'https://api.test',
    store: memoryStore(),
    relogin: async () => {
      relogins += 1
    }
  })
  await assert.rejects(() => client.request('/v1/auth/sms/verify', { method: 'POST', data: {} }))
  assert.equal(relogins, 0)
})

test('请求头注入 Bearer 与 content-type json；timeout 透传', async () => {
  const seen = []
  const { adapter } = fakeAdapter((req) => {
    seen.push(req)
    return { statusCode: 200, data: { data: 1 } }
  })
  const client = createApiClient({ adapter, baseUrl: 'https://api.test', store: memoryStore('tok') })
  await client.request('/v1/me', { timeout: 8000 })
  const req = seen[0]
  assert.equal(req.header.Authorization, 'Bearer tok')
  assert.equal(req.header['content-type'], 'application/json')
  assert.equal(req.timeout, 8000)
  assert.equal(req.method, 'GET')
})

test('204/裸值响应：data 为 undefined/原值，不误吞', async () => {
  const paths = new Map([
    ['/a', { statusCode: 204, data: undefined }],
    ['/b', { statusCode: 200, data: { data: null } }],
    ['/c', { statusCode: 200, data: [1, 2] }]
  ])
  const { adapter } = fakeAdapter((req) => paths.get(req.path))
  const client = createApiClient({ adapter, baseUrl: 'https://api.test' })
  assert.equal(await client.request('/a'), undefined)
  assert.equal(await client.request('/b'), null)
  assert.deepEqual(await client.request('/c'), [1, 2])
})

test('uploadFile：字符串响应体 JSON.parse + 401 重试一次', async () => {
  let state = 'stale'
  const uploads = []
  const adapter = { request: async () => ({ statusCode: 200, data: { data: 1 } }) }
  const upload = {
    async upload(req) {
      uploads.push(req)
      if (state === 'stale') return { statusCode: 401, data: JSON.stringify({ error: { code: 'unauthorized', message: 'x' } }) }
      return { statusCode: 201, data: JSON.stringify({ data: { id: 'm1', kind: 'face', url: 'https://x/1.jpg', created_at: 'now' } }) }
    }
  }
  const store = memoryStore('stale')
  const client = createApiClient({
    adapter,
    upload,
    baseUrl: 'https://api.test',
    store,
    relogin: async () => {
      state = 'good'
      store.set('good')
    }
  })
  const media = await client.uploadFile({ path: '/v1/media', filePath: '/tmp/f.jpg', formData: { kind: 'face' } })
  assert.equal(media.id, 'm1')
  assert.equal(uploads.length, 2)
  assert.equal(uploads[0].header.Authorization, 'Bearer stale')
  assert.equal(uploads[1].header.Authorization, 'Bearer good')
})

test('localizeDevImages 中间件：深遍历替换 http 图片，https 直通，无注入直通', async () => {
  const mw = localizeDevImages(async (url) => (url.endsWith('ok.jpg') ? `wxfile://${url.length}.jpg` : ''))
  const client = createApiClient({
    adapter: fakeAdapter(() => ({
      statusCode: 200,
      data: {
        data: {
          image_url: 'http://dev.local:58000/ok.jpg',
          nested: { list: [{ url: 'http://dev.local:58000/bad.jpg' }, { url: 'https://cdn/keep.jpg' }] }
        }
      }
    })).adapter,
    baseUrl: 'http://dev.local:58000',
    middleware: [mw]
  })
  const data = await client.request('/v1/home/bootstrap')
  const okUrl = 'http://dev.local:58000/ok.jpg'
  assert.equal(data.image_url, `wxfile://${okUrl.length}.jpg`)
  assert.equal(data.nested.list[0].url, '') // 下载失败 → 置空（页面显示空态）
  assert.equal(data.nested.list[1].url, 'https://cdn/keep.jpg')
  // 无注入：直通
  const passthrough = localizeDevImages()
  assert.deepEqual(await passthrough({ a: 'http://x/1.jpg' }), { a: 'http://x/1.jpg' })
})

test('rewriteLoopbackAssetURLs：生产 API 下把 127.0.0.1/uploads 改到 API 域名', async () => {
  const mw = rewriteLoopbackAssetURLs('https://prompt.wuyill.com/zhanshimian')
  const out = await mw({
    url: 'http://127.0.0.1:58000/uploads/u/a.png',
    nested: { src: 'http://localhost:58000/uploads/u/b.jpg' },
    other: 'http://127.0.0.1:58000/v1/healthz',
    https: 'https://cdn.example/keep.jpg',
  })
  assert.equal(out.url, 'https://prompt.wuyill.com/zhanshimian/uploads/u/a.png')
  assert.equal(out.nested.src, 'https://prompt.wuyill.com/zhanshimian/uploads/u/b.jpg')
  assert.equal(out.other, 'http://127.0.0.1:58000/v1/healthz')
  assert.equal(out.https, 'https://cdn.example/keep.jpg')
  const local = rewriteLoopbackAssetURLs('http://127.0.0.1:58000')
  assert.equal((await local({ url: 'http://127.0.0.1:58000/uploads/x.png' })).url, 'http://127.0.0.1:58000/uploads/x.png')
})

test('diagnose：404 清 report 引用后无 report_id 重试一次', async () => {
  const posts = []
  let clearCalls = 0
  const { adapter } = fakeAdapter((req) => {
    posts.push(req)
    if (req.data?.report_id) {
      return { statusCode: 404, data: { error: { code: 'report_not_found', message: '报告不存在' } } }
    }
    return { statusCode: 201, data: { data: { id: 'd1', kind: req.data.kind, saved: false } } }
  })
  const client = createApiClient({ adapter, baseUrl: 'https://api.test' })
  const api = createApiEndpoints(client, { clearReportRef: () => { clearCalls += 1 } })
  const diagnosis = await api.diagnose({ kind: 'outfit', media_id: 'm1', report_id: 'gone' })
  assert.equal(diagnosis.id, 'd1')
  assert.equal(clearCalls, 1)
  assert.equal(posts.length, 2)
  assert.equal(posts[1].data.report_id, undefined)
})
