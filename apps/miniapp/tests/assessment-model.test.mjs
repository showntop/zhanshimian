// 分析页的三条硬规则：进度只认服务端说的阶段码，照片只来自本次提交，
// 失败只按 retryable 分成「重新发起」和「重新拍摄」两种恢复动作。
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  assessmentEndedBody,
  assessmentPhotoSlots,
  assessmentPhotosFrom,
  assessmentRecovery,
  assessmentRetryKey,
  assessmentRoute,
  assessmentStageLine,
  assessmentStepOf,
} from '../src/features/assessment/model.ts'
import { ASSESSMENT_STAGE_COPY } from '@zsm/core'

const display = (id) => ({
  asset_id: id,
  url: `https://cdn.example/${id}.jpg`,
  url_expires_at: '2099-01-01T00:00:00Z',
  mime_type: 'image/jpeg',
  source_kind: 'user_original',
  display_label: '原本',
})

// 服务端 assessment policy 的全部阶段码（apps/server/internal/service/assessment/policy.go）
const SERVER_STAGES = [
  'photo.technical_check',
  'photo.content_check',
  'photo.identity_check',
  'report.generating',
  'report.evidence_check',
  'report.publishing',
]

test('every server stage code maps to a known step and a known copy line', () => {
  // 两张表错位的表现是「文案说在核对照片，指示器却亮着整理报告」——
  // 从截图上几乎看不出来，只能靠这条断言把键集合钉在一起。
  for (const stage of SERVER_STAGES) {
    assert.notEqual(assessmentStepOf(stage), -1, `${stage} has no step`)
    assert.equal(
      Object.hasOwn(ASSESSMENT_STAGE_COPY, stage),
      true,
      `${stage} has no copy line`,
    )
  }
  assert.deepEqual(
    Object.keys(ASSESSMENT_STAGE_COPY).sort(),
    [...SERVER_STAGES].sort(),
    'copy table and step table disagree about which stages exist',
  )
})

test('steps move forward across the pipeline and unknown codes light nothing', () => {
  assert.equal(assessmentStepOf('photo.technical_check'), 0)
  assert.equal(assessmentStepOf('photo.identity_check'), 0)
  assert.equal(assessmentStepOf('report.generating'), 1)
  assert.equal(assessmentStepOf('report.publishing'), 2)
  // 服务端加了新阶段码而客户端还没同步时，三步全不亮——
  // 好过把用户指到某一步上，假装知道现在在做什么。
  assert.equal(assessmentStepOf('report.unknown_phase'), -1)
  assert.equal(assessmentStepOf(''), -1)
  assert.equal(assessmentStepOf(undefined), -1)
})

test('the progress line is the server public message first, stage copy only as fallback', () => {
  assert.equal(
    assessmentStageLine({ message: '正在核对照片清晰度', stageCode: 'photo.technical_check' }),
    '正在核对照片清晰度',
  )
  // 服务端没给公开文案时按阶段码查表，绝不按已过时间编第二套说法
  assert.equal(
    assessmentStageLine({ message: '', stageCode: 'report.publishing' }),
    ASSESSMENT_STAGE_COPY['report.publishing'],
  )
  assert.equal(assessmentStageLine({ message: '', stageCode: '' }), '正在分析，请稍候')
})

test('cached photos keep their roles and never invent a missing one', () => {
  const cached = { face: display('face-1'), side: display('side-1') }
  assert.deepEqual(assessmentPhotosFrom(cached), cached)

  const slots = assessmentPhotoSlots(cached)
  assert.deepEqual(
    slots.map((slot) => slot.role),
    ['face', 'side', 'body'],
  )
  assert.equal(slots[0].media.asset_id, 'face-1')
  // 全身照不在缓存里就是空槽：不拿别的角色顶上，也不拿内置图顶上
  assert.equal(slots[2].media, null)

  // 冷启动（缓存被清空）与形状不对的旧值都读成空
  assert.deepEqual(assessmentPhotosFrom(undefined), {})
  assert.deepEqual(assessmentPhotosFrom('nope'), {})
  assert.deepEqual(assessmentPhotosFrom({ face: { asset_id: 'x' } }), {})
  assert.deepEqual(assessmentPhotosFrom({ face: { asset_id: 'x', url: '' } }), {})
})

test('failure splits into retry and reshoot by retryability alone', () => {
  const retryable = assessmentRecovery({ message: '这次分析没有完成', retryable: true })
  assert.equal(retryable.action, 'retry')
  assert.equal(retryable.actionText, '重新发起')
  assert.equal(retryable.body, '这次分析没有完成')

  const rejected = assessmentRecovery({ message: '', retryable: false })
  assert.equal(rejected.action, 'reshoot')
  assert.equal(rejected.actionText, '重新拍摄')
  assert.equal(rejected.title, '照片没有通过检查')
  assert.equal(rejected.body, '请按拍摄指引重新提交')
})

test('a retry never reuses the first submission key', () => {
  const photos = { face: display('face-1'), side: display('side-1'), body: display('body-1') }
  // 首提的键已被服务端和那次失败的受理绑在一起：复用只会把同一份失败原样重放回来，
  // 用户按一百次也还是那一份结果。所以重发必须换键，且换出来的键要能区分第几次。
  const first = assessmentRetryKey(photos, 1)
  assert.notEqual(first, 'assessment:face-1:side-1:body-1')
  assert.notEqual(first, assessmentRetryKey(photos, 2))
  assert.equal(first, assessmentRetryKey(photos, 1))
  assert.match(first, /face-1/)
})

test('cancelled and superseded explain themselves differently', () => {
  assert.notEqual(assessmentEndedBody('cancelled'), assessmentEndedBody('superseded'))
  assert.match(assessmentEndedBody('cancelled'), /取消/)
  assert.match(assessmentEndedBody('superseded'), /替代/)
})

test('the progress route carries both ids, and escapes them', () => {
  assert.equal(
    assessmentRoute({ data: { id: 'a-1' }, operation: { id: 'op-1' } }),
    '/pages/analysis/index?assessment_id=a-1&operation_id=op-1',
  )
  // id 进了 query，就得按 query 转义——否则一个带 & 的 id 会把参数拆成两半
  assert.match(
    assessmentRoute({ data: { id: 'a&b' }, operation: { id: 'o p' } }),
    /\?assessment_id=a%26b&operation_id=o%20p$/,
  )
})
