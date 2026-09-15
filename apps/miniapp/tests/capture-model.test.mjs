import test from 'node:test'
import assert from 'node:assert/strict'
import {
  batchAssignmentRoles,
  batchChooseCount,
  captureReady,
  toAssessmentInput,
  assessmentIdempotencyKey,
  createSlots,
  updateSlot,
  photosByRole,
} from '../src/features/capture/model.ts'

// 夹具日期取远未来常量，不写「今天 + 一天」：捕获模型现在不读 url_expires_at，
// 但一旦哪天把夹具喂进 projectDisplayMedia，写死的近期日期会在某天悄悄变红。
const display = (id, kind = 'user_original') => ({
  asset_id: id,
  url: `https://cdn.example/${id}.jpg`,
  url_expires_at: '2099-01-01T00:00:00Z',
  mime_type: 'image/jpeg',
  source_kind: kind,
  display_label: kind === 'demo_example' ? '效果示例' : '原本',
})

test('requires exactly one distinct face, side and body asset', () => {
  assert.equal(
    captureReady({ face: display('f'), side: display('s'), body: display('b') }),
    true,
  )
  assert.equal(captureReady({ face: display('f'), side: display('s') }), false)
  assert.equal(
    captureReady({ face: display('same'), side: display('same'), body: display('b') }),
    false,
  )
})

test('assessment input preserves explicit photo roles and lets the server snapshot profile', () => {
  assert.deepEqual(
    toAssessmentInput({ face: display('f'), side: display('s'), body: display('b') }),
    {
      photos: {
        face_asset_id: 'f',
        side_asset_id: 's',
        body_asset_id: 'b',
      },
    },
  )
})

test('assessment idempotency key is derived from the three asset ids, not from time', () => {
  const photos = { face: display('f'), side: display('s'), body: display('b') }
  const same = { face: display('f'), side: display('s'), body: display('b') }
  assert.equal(assessmentIdempotencyKey(photos), assessmentIdempotencyKey(same))
  assert.notEqual(
    assessmentIdempotencyKey(photos),
    assessmentIdempotencyKey({ face: display('f'), side: display('s'), body: display('b2') }),
  )
  assert.match(assessmentIdempotencyKey(photos), /^assessment:/)
})

test('idempotency key and assessment input refuse to fabricate ids for missing slots', () => {
  assert.throws(() => toAssessmentInput({ face: display('f'), side: display('s') }))
  assert.throws(() => assessmentIdempotencyKey({}))
})

test('a slot only counts once it is ready, so a failed re-shoot blocks submit', () => {
  let slots = createSlots()
  assert.deepEqual(photosByRole(slots), {})
  assert.equal(captureReady(photosByRole(slots)), false)

  slots = updateSlot(slots, 'face', { phase: 'ready', media: display('f') })
  slots = updateSlot(slots, 'side', { phase: 'ready', media: display('s') })
  slots = updateSlot(slots, 'body', { phase: 'ready', media: display('b') })
  assert.equal(captureReady(photosByRole(slots)), true)

  // 重拍失败：老照片还在槽里，但状态不是 ready，提交必须被挡住
  slots = updateSlot(slots, 'body', { phase: 'failed', errorText: '这张没有上传成功' })
  assert.equal(captureReady(photosByRole(slots)), false)
  assert.equal(slots.body.errorText, '这张没有上传成功')
})

test('a ready slot must carry media and a non-ready slot must not claim to', () => {
  const slots = createSlots()
  assert.throws(() => updateSlot(slots, 'face', { phase: 'ready' }))
  assert.equal(updateSlot(slots, 'face', { phase: 'hashing', localPath: 'wxfile://t1' }).face.media, null)
})

test('batch choose count is one for camera and the full missing count for album', () => {
  // 微信相机模式一次拍摄只返回 1 个临时文件，count 写多大都只给一张
  assert.equal(batchChooseCount('camera', 3), 1)
  assert.equal(batchChooseCount('camera', 1), 1)
  assert.equal(batchChooseCount('album', 3), 3)
  assert.equal(batchChooseCount('album', 2), 2)
})

test('batch assignment fills only the slots that actually got a file', () => {
  // 单拍/少选时，没回到文件的槽保持原状，不能给它们记 false 误弹部分失败
  assert.deepEqual(batchAssignmentRoles(['side', 'body'], 1), ['side'])
  assert.deepEqual(batchAssignmentRoles(['face', 'side', 'body'], 2), ['face', 'side'])
  assert.deepEqual(batchAssignmentRoles(['face', 'side', 'body'], 3), ['face', 'side', 'body'])
  assert.deepEqual(batchAssignmentRoles(['face'], 0), [])
})
