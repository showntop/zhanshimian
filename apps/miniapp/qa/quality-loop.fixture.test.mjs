// 64 个「来源 × 状态」组合的回归证明：来源投影与状态投影都只认服务端声明。
// 这条测试是质量闭环的最后一道网：任何一处投影规则的回归都会在这里点名到具体 id。
import test from 'node:test'
import assert from 'node:assert/strict'
import { QUALITY_LOOP_FIXTURES } from './fixtures/quality-loop.ts'
import { projectDisplayMedia } from '../../../packages/core/src/media/display.ts'
import { planSetView, variantRenderView } from '../src/features/planning/model.ts'
import { operationView } from '../src/app/operations/operation-view.ts'

function projectState(fixture) {
  switch (fixture.stateDomain) {
    case 'plan-set':
      return planSetView(fixture.stateInput)
    case 'render':
      return variantRenderView(fixture.stateInput)
    case 'operation':
      return operationView(fixture.stateInput)
    default:
      throw new Error(`unknown state domain: ${fixture.stateDomain}`)
  }
}

test('fixture set contains 64 explicit source/state cases', () => {
  assert.equal(QUALITY_LOOP_FIXTURES.length, 64)
  assert.equal(new Set(QUALITY_LOOP_FIXTURES.map((item) => item.id)).size, 64)
})

test('every fixture has deterministic media and plan-set projection', () => {
  for (const fixture of QUALITY_LOOP_FIXTURES) {
    assert.deepEqual(
      projectDisplayMedia(fixture.media, fixture.now),
      fixture.expectedMedia,
      fixture.id,
    )
    assert.deepEqual(
      projectState(fixture),
      fixture.expectedState,
      fixture.id,
    )
  }
})
