import test from 'node:test'
import assert from 'node:assert/strict'
import { createResourceCache, mediaKey, resourceKey } from '../src/app/cache/resource-cache.ts'

test('resources are partitioned by type and resource id', () => {
  const cache = createResourceCache()
  cache.write(resourceKey('report', 'r1'), { id: 'r1' })
  cache.write(resourceKey('plan-set', 'p1'), { id: 'p1' })
  assert.deepEqual(cache.read(resourceKey('report', 'r1')), { id: 'r1' })
  assert.equal(cache.read(resourceKey('report', 'p1')), undefined)
})

test('media cache includes asset id and signed url', () => {
  assert.notEqual(
    mediaKey({ asset_id: 'a1', url: 'https://cdn/a1.jpg?sig=one' }),
    mediaKey({ asset_id: 'a1', url: 'https://cdn/a1.jpg?sig=two' }),
  )
  assert.notEqual(
    mediaKey({ asset_id: 'a1', url: 'https://cdn/a.jpg' }),
    mediaKey({ asset_id: 'a2', url: 'https://cdn/a.jpg' }),
  )
})

test('invalid server refresh replaces cached resource instead of pinning old media', async () => {
  const cache = createResourceCache()
  const key = resourceKey('plan-set', 'p1')
  cache.write(key, { id: 'p1', plans: [{ id: 'v1', render: { media: { asset_id: 'old' } } }] })
  await cache.revalidate(key, async () => ({ id: 'p1', plans: [{ id: 'v1', render: { media: null } }] }))
  assert.equal(cache.read(key).plans[0].render.media, null)
})

test('subscribers are notified on write and remove, and unsubscribe stops them', () => {
  const cache = createResourceCache()
  const key = resourceKey('operation', 'op-1')
  let seen = 0
  const unsubscribe = cache.subscribe(key, () => {
    seen += 1
  })
  cache.write(key, { id: 'op-1' })
  cache.remove(key)
  unsubscribe()
  cache.write(key, { id: 'op-1' })
  assert.equal(seen, 2)
  assert.equal(cache.read(key).id, 'op-1')
})

test('concurrent revalidations of one key collapse into a single request', async () => {
  const cache = createResourceCache()
  const key = resourceKey('execution', 'e1')
  let calls = 0
  const load = async () => {
    calls += 1
    return { id: 'e1', version: calls }
  }
  const [first, second] = await Promise.all([cache.revalidate(key, load), cache.revalidate(key, load)])
  assert.equal(calls, 1)
  assert.deepEqual(first, second)
  await cache.revalidate(key, load)
  assert.equal(calls, 2)
})
