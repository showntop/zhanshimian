// 反馈请求体的两条硬规则：标签就是契约枚举本身（不传中文给服务端），
// 没有上传成功的照片就整段省略 media_asset_id——契约不允许 null，也不许占位。
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  executionFeedbackBody,
  generationFeedbackBody,
  generationTagLabel,
  executionTagLabel,
} from '../src/features/feedback/model.ts'

test('generation feedback binds the publication the user saw', () => {
  assert.deepEqual(
    generationFeedbackBody(
      { publication_id: 'publication-1' },
      ['identity_mismatch', 'hair_mismatch'],
      '',
      null,
    ),
    {
      publication_id: 'publication-1',
      tags: ['identity_mismatch', 'hair_mismatch'],
    },
  )
})

test('execution feedback remains valid without a photo', () => {
  const body = executionFeedbackBody(
    { execution_id: 'e1' },
    ['easy_to_execute', 'want_to_keep'],
    '发型很省时间',
    null,
  )
  // 没有上传成功的照片：media_asset_id 整段省略，但文字照常提交
  assert.equal('media_asset_id' in body, false)
  assert.equal(body.comment, '发型很省时间')
  assert.deepEqual(body.tags, ['easy_to_execute', 'want_to_keep'])
})

test('comment and media are included only when they exist', () => {
  // 契约里 media_asset_id 是 uuid 字符串、不可空：没上传成功就省略，不发 null 占位
  const withEverything = generationFeedbackBody(
    { publication_id: 'p1' },
    ['unnatural'],
    '手部有点奇怪',
    'asset-1',
  )
  assert.deepEqual(withEverything, {
    publication_id: 'p1',
    tags: ['unnatural'],
    comment: '手部有点奇怪',
    media_asset_id: 'asset-1',
  })
  const withoutComment = executionFeedbackBody(
    { execution_id: 'e1' },
    ['too_formal'],
    '',
    'asset-2',
  )
  assert.deepEqual(withoutComment, {
    execution_id: 'e1',
    tags: ['too_formal'],
    media_asset_id: 'asset-2',
  })
})

test('tags round-trip between contract enums and screen labels', () => {
  assert.equal(generationTagLabel('identity_mismatch'), '不像本人')
  assert.equal(generationTagLabel('unnatural'), '不够自然')
  assert.equal(executionTagLabel('easy_to_execute'), '容易执行')
  assert.equal(executionTagLabel('want_to_keep'), '希望保留')
  // 表外值显示原值：不抛错，不编一个标签
  assert.equal(generationTagLabel('something_new'), 'something_new')
})
