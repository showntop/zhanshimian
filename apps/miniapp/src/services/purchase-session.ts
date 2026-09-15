// 购买判断会话：同步请求跨页存活——模块级 inflight Promise + storage 草稿。
// 与 outfit-session 同构（旧线双文件模式）：差别只在没有场景、存储键不同。
// 发起后离开再进：进行中接管等待（不重新发起），完成有结果，失败有错误态；
// 复访先展示草稿，再用服务端 latest 后台校验（只在更新时接管，绝不旧盖新）。
//
// 状态机：idle（无草稿/草稿无 pending）→ pending（markPending，请求在途）
//   → done（markDone，result 落草稿）/ idle（markIdle，失败落定不再自动续跑）。
// 存储键 zsm_purchase_session：这只是「这一页在干什么」的会话草稿，服务端
// latest 才是结论事实——草稿丢了从头再来，不会拿到错的数据。
//
// 不 import Taro：存储由调用方注入（页面传 services/storage 的 read/write），
// node --test 因此可以直接加载本文件（@tarojs/taro 在 node 里起不来，
// 同 billing-error.ts 的 .ts 后缀理由）。
import type { Diagnosis } from '@zsm/core'
import { billingErrorKind } from './billing-error.ts'

/**
 * 诊断日限（8 次/日）走 429/402：调用方据此给「明日再来/查看历史」的体面错误态。
 * diagnostic 不在计费购买链——402 在这里是日限，不是「额度不足去买次数」，
 * 所以不能走 handleBillingError 的购买层；归类复用 billing-error 的纯分类器。
 */
export function isDiagnosticDailyLimit(error: unknown): boolean {
  const kind = billingErrorKind(error)
  return kind === 'rate_limited' || kind === 'insufficient_credits'
}

export interface PurchaseDraft {
  pending: boolean
  /** 本地临时路径：只在本机本次运行里可预览，冷启动后可能失效（草稿仍带 mediaId 续跑）。 */
  photoPath: string
  /** 已上传的媒体 id：pending 草稿续跑与 sameMedia 判定的锚。 */
  mediaId: string
  startedAt: number
  result: Diagnosis | null
}

const emptyDraft = (): PurchaseDraft => ({
  pending: false,
  photoPath: '',
  mediaId: '',
  startedAt: 0,
  result: null,
})

/** 存储适配：页面侧传 services/storage 的 readStorage/writeStorage。 */
export interface PurchaseSessionStore {
  read(key: string): string
  write(key: string, value: string): void
}

/** 复访事件：页面据此 setState；测试据此断言时序与分支。 */
export type PurchaseResumeEvent =
  | { type: 'hydrate'; draft: PurchaseDraft }
  | { type: 'busy'; busy: boolean }
  | { type: 'result'; item: Diagnosis }
  | { type: 'error'; error: unknown }

export interface PurchaseSessionDeps {
  getLatest: () => Promise<Diagnosis | null>
  diagnose: (input: { kind: 'purchase'; media_id: string; report_id?: string }) => Promise<Diagnosis>
}

/**
 * 进行中的请求 vs 服务端 latest：够新且同一照片才接管，否则忽略旧结论（旧线判定）。
 * 新契约 Diagnosis 没有 media_id 字段，照片锚用 source_media.asset_id；
 * latest 没带照片时退回纯时间判定（与旧线 `!latest.media_id` 的宽容一致）。
 */
export function shouldAdoptLatest(
  draft: Pick<PurchaseDraft, 'startedAt' | 'mediaId'>,
  latest: Diagnosis,
): boolean {
  const startedAt = draft.startedAt || 0
  const recentEnough = new Date(latest.created_at).getTime() >= startedAt - 5000
  const latestMedia = latest.source_media?.asset_id ?? ''
  const sameMedia = !draft.mediaId || !latestMedia || draft.mediaId === latestMedia
  return recentEnough && sameMedia
}

/** 后台校验只在服务端结论更新时接管；同一条或更旧都保持草稿原样（防旧盖新）。 */
export function isNewerDiagnosis(latest: Diagnosis, current: Diagnosis): boolean {
  if (latest.id === current.id) return false
  return new Date(latest.created_at).getTime() > new Date(current.created_at).getTime()
}

export function createPurchaseSession(store: PurchaseSessionStore, storageKey = 'zsm_purchase_session') {
  let memory: PurchaseDraft | null = null
  let inflight: Promise<Diagnosis> | null = null

  function persist(next: PurchaseDraft) {
    memory = next
    store.write(storageKey, JSON.stringify(next))
  }

  function read(): PurchaseDraft | null {
    if (memory) return memory
    try {
      const raw = store.read(storageKey)
      if (raw) {
        const parsed = JSON.parse(raw) as PurchaseDraft
        if (parsed && typeof parsed === 'object') {
          memory = { ...emptyDraft(), ...parsed }
          return memory
        }
      }
    } catch {
      /* 读失败当无会话 */
    }
    return null
  }

  function writeDraft(patch: Partial<PurchaseDraft>) {
    persist({ ...emptyDraft(), ...read(), ...patch })
  }

  /** 请求发起前一刻才标 pending：pending + mediaId 表示「有一趟针对这张图的请求在途」。 */
  function markPending(patch: Partial<PurchaseDraft>) {
    persist({ ...emptyDraft(), ...read(), ...patch, pending: true, startedAt: Date.now(), result: null })
  }

  function markDone(result: Diagnosis) {
    persist({ ...emptyDraft(), ...read(), pending: false, result })
  }

  function markIdle() {
    const prev = read()
    if (prev) persist({ ...prev, pending: false })
  }

  function clearResult() {
    const prev = read()
    if (prev) persist({ ...prev, pending: false, result: null })
  }

  /**
   * 同一时刻只允许一趟判断请求；inflight 期间的发起一律接管同一个 Promise。
   * mediaId 是这趟请求针对的图：落定（done/idle）只在草稿仍指向同一张图时写盘——
   * 飞行中用户重选了图，迟到的旧结论就不许盖掉新草稿（防旧盖新的收口）。
   */
  function runDiagnose(mediaId: string, run: () => Promise<Diagnosis>): Promise<Diagnosis> {
    if (inflight) return inflight
    inflight = run()
      .then((item) => {
        const draft = read()
        if (draft?.pending && draft.mediaId === mediaId) markDone(item)
        return item
      })
      .catch((error: unknown) => {
        const draft = read()
        if (draft?.pending && draft.mediaId === mediaId) markIdle()
        throw error
      })
      .finally(() => {
        inflight = null
      })
    return inflight
  }

  /** 结果只有被会话收养（草稿仍指向同一张图）才值得上屏。 */
  function adopted(item: Diagnosis): boolean {
    return read()?.result?.id === item.id
  }

  /**
   * 复访恢复时序：
   * 1) 草稿水合先上屏（photoPath/已有结论）；
   * 2) 有 inflight 就接管等待，绝不重新发起；
   * 3) pending 草稿：先问服务端 latest（recentEnough && sameMedia 才接管），
   *    查不到或不接管再用同一 mediaId 续跑——中途换图则放弃上屏与落盘；
   * 4) 其余情况后台校验 latest：只在比草稿新时接管；freshStart（刚点了再来一次/
   *    重选图）不查，免得服务端旧结论立刻盖回。
   */
  async function resume(
    deps: PurchaseSessionDeps,
    emit: (event: PurchaseResumeEvent) => void,
    opts: { freshStart?: () => boolean } = {},
  ): Promise<void> {
    const freshStart = () => opts.freshStart?.() === true
    const draft = read()
    if (draft) emit({ type: 'hydrate', draft })

    const pending = inflight
    if (pending) {
      emit({ type: 'busy', busy: true })
      try {
        const item = await pending
        if (adopted(item)) emit({ type: 'result', item })
      } catch (error) {
        emit({ type: 'error', error })
      } finally {
        emit({ type: 'busy', busy: false })
      }
      return
    }

    if (draft?.pending && draft.mediaId) {
      emit({ type: 'busy', busy: true })
      try {
        let latest: Diagnosis | null = null
        try {
          latest = await deps.getLatest()
        } catch {
          /* 服务端还没有这条结论：用同一张图续跑判断 */
        }
        if (latest && shouldAdoptLatest(draft, latest)) {
          const now = read()
          if (now?.pending && now.mediaId === draft.mediaId) {
            markDone(latest)
            emit({ type: 'result', item: latest })
          }
          return
        }
        const item = await runDiagnose(draft.mediaId, () =>
          deps.diagnose({ kind: 'purchase', media_id: draft.mediaId }),
        )
        if (adopted(item)) emit({ type: 'result', item })
      } catch (error) {
        emit({ type: 'error', error })
      } finally {
        emit({ type: 'busy', busy: false })
      }
      return
    }

    if (freshStart()) return

    let latest: Diagnosis | null = null
    try {
      latest = await deps.getLatest()
    } catch {
      return
    }
    if (!latest) return
    if (freshStart()) return
    const current = read()?.result
    if (current && !isNewerDiagnosis(latest, current)) return
    markDone(latest)
    emit({ type: 'result', item: latest })
  }

  return {
    read,
    writeDraft,
    markPending,
    markDone,
    markIdle,
    clearResult,
    getInflight: () => inflight,
    runDiagnose,
    resume,
  }
}

export type PurchaseSession = ReturnType<typeof createPurchaseSession>

/** 页面共享单例：页面模块只装载一次，模块级 inflight 因此跨页存活。 */
let shared: PurchaseSession | null = null
export function sharedPurchaseSession(store: PurchaseSessionStore): PurchaseSession {
  if (!shared) shared = createPurchaseSession(store)
  return shared
}
