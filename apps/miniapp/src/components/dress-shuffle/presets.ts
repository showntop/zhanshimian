// 换装洗牌 · 维度库与洗牌纯函数（从原型 daily-dressup-asset.html 移植）。
//
// 判归规则与命名见 docs/superpowers/specs/2026-09-23-dress-shuffle-design.md。
// 本模块不碰组件/Taro：全部可单测。

// ---------- 维度库 ----------

export interface LookItem {
  name: string
  key: string
}

export interface ColorItem {
  name: string
  hex: string
  /** 基准砖红 → 目标色的 WXSS 滤镜（数值经矩阵优化拟合，见规范 §四） */
  filter: string
}

export interface WaistItem {
  name: string
  pct: number
}

export interface HairItem {
  name: string
  key: string
}

export const LOOKS: LookItem[] = [
  { name: '针织+阔腿裤', key: 'outfit' },
  { name: '短上衣+高腰裤', key: 'ratio' },
  { name: 'A字连衣裙', key: 'fit' },
  { name: '西装叠穿', key: 'occasion' },
  { name: '衬衫+半裙', key: 'look-shirt-skirt' },
  { name: '卫衣+牛仔裤', key: 'look-hoodie-jeans' },
  { name: '毛呢大衣', key: 'look-coat' },
  { name: '风衣+围巾', key: 'look-trench' },
]

/** 基准色 = 砖红（母版原色，filter none）；深色四张 = 专属生图（spec §三 步骤 5） */
export const COLORS: ColorItem[] = [
  { name: '燕麦', hex: '#D9D0C1', filter: 'hue-rotate(35deg) saturate(.1) brightness(2.2)' },
  { name: '砖红', hex: '#B4553C', filter: 'none' },
  { name: '墨绿', hex: '#3F5340', filter: 'hue-rotate(98deg) saturate(.9) brightness(.82)' },
  { name: '藏蓝', hex: '#2F3E56', filter: 'hue-rotate(200deg) saturate(.85) brightness(.8)' },
  { name: '浅灰', hex: '#C6C4BE', filter: 'hue-rotate(80deg) saturate(.1) brightness(2.15)' },
  { name: '驼色', hex: '#B08B5E', filter: 'hue-rotate(30deg) saturate(.45) brightness(1.5)' },
  { name: '雾蓝', hex: '#8FA6B8', filter: 'hue-rotate(230deg) saturate(.25) brightness(1.6)' },
  { name: '酒红', hex: '#7B2D35', filter: 'hue-rotate(-21deg) saturate(1.05) brightness(.72)' },
  { name: '橄榄', hex: '#75754B', filter: 'hue-rotate(60deg) saturate(.35) brightness(1.2)' },
  { name: '炭灰', hex: '#3A3D42', filter: 'hue-rotate(170deg) saturate(.45) brightness(.55)' },
  { name: '奶油白', hex: '#F1EAD8', filter: 'hue-rotate(50deg) saturate(.1) brightness(2.5)' },
  { name: '粉棕', hex: '#C08A82', filter: 'hue-rotate(355deg) saturate(.35) brightness(1.55)' },
]

export const WAISTS: WaistItem[] = [
  { name: '高腰', pct: 46 },
  { name: '中腰', pct: 52 },
  { name: '低腰', pct: 58 },
]

export const HAIRS: HairItem[] = [
  { name: '大波浪', key: 'wave' },
  { name: '丸子头', key: 'bun' },
  { name: '齐耳短发', key: 'bob' },
  { name: '长直发', key: 'long' },
]

/** 每维度洗牌入池数量（< 库存量 = 随机子集；target 保底入池） */
export const POOL_SIZE: Record<DimKey, number> = { look: 4, color: 5, waist: 3, hair: 3 }

// ---------- target（服务端 dress_lock 下发；本地循环轮随机生成用） ----------

export interface DressTarget {
  look: number
  color: number
  waist: number
  hair: number
}

// ---------- 洗牌纯函数 ----------

export type DimKey = 'look' | 'color' | 'waist' | 'hair'

export type CardKind = 'look' | 'swatch' | 'rule' | 'hair'

export interface ShuffleItem {
  kind: CardKind
  name: string
  /** look/hair 的素材键 */
  key?: string
  /** swatch 的色值 */
  hex?: string
  /** rule 的腰线位置 */
  pct?: number
}

export interface RunDim {
  key: DimKey
  label: string
  badge: string
  items: ShuffleItem[]
  /** target 在 items 中的下标（保底入池后重算） */
  target: number
}

function shuffleIdx(n: number): number[] {
  const idx = [...Array(n).keys()]
  for (let i = n - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    const a = at(idx, i)
    idx[i] = at(idx, j)
    idx[j] = a
  }
  return idx
}

/** 下标访问断言：调用点保证下标合法（洗牌/取模/pickPool 保底） */
const at = <T,>(arr: T[], i: number): T => arr[i] as T

/** 随机子集：target 保底入池，池内顺序也洗（反单调的核心机制） */
/** 随机子集：target 保底入池，池内顺序也洗（反单调的核心机制） */
export function pickPool(len: number, n: number, targetIdx: number): number[] {
  const size = Math.min(n, len)
  const rest = shuffleIdx(len).filter((i) => i !== targetIdx)
  const pool = rest.slice(0, size - 1)
  pool.push(targetIdx)
  return shuffleIdx(pool.length).map((i) => at(pool, i))
}

const DIM_LIBS: Record<DimKey, { items: ShuffleItem[]; label: string; badge: string }> = {
  look: {
    label: '在试造型',
    badge: '造型',
    items: LOOKS.map((l) => ({ kind: 'look' as const, name: l.name, key: l.key })),
  },
  color: {
    label: '在调配色',
    badge: '配色',
    items: COLORS.map((c) => ({ kind: 'swatch' as const, name: c.name, hex: c.hex })),
  },
  waist: {
    label: '在找腰线',
    badge: '比例',
    items: WAISTS.map((w) => ({ kind: 'rule' as const, name: w.name, pct: w.pct })),
  },
  hair: {
    label: '在换发型',
    badge: '发型',
    items: HAIRS.map((h) => ({ kind: 'hair' as const, name: h.name, key: h.key })),
  },
}

const DIM_KEYS: DimKey[] = ['look', 'color', 'waist', 'hair']

/**
 * 构建一轮洗牌：每维度随机子集 + target 保底。
 *
 * 发型维度只在 target.look === 0（hero look）时入列——发型素材只做了
 * hero look；其他 look 的日子该维度自动跳过（等待轮与收敛轮同规则）。
 * hairAvailable=false（发型素材缺失/未就绪）时整维度不出池（spec §4）。
 *
 * @param opts.force          强制某维度先手（单维度验收）
 * @param opts.order          固定维度顺序（收敛轮：look→color→waist→hair）
 * @param opts.hairAvailable  发型素材是否可用（默认 true）
 */
export function buildRunDims(
  target: DressTarget,
  opts: { force?: DimKey; order?: DimKey[]; hairAvailable?: boolean } = {},
): RunDim[] {
  let keys = DIM_KEYS.filter((k) => k !== 'hair' || (target.look === 0 && opts.hairAvailable !== false))
  if (opts.order) keys = opts.order.filter((k) => keys.includes(k))
  else keys = shuffleIdx(keys.length).map((i) => at(keys, i))
  if (opts.force) {
    const at0 = keys.indexOf(opts.force)
    if (at0 > 0) {
      keys.splice(at0, 1)
      keys.unshift(opts.force)
    }
  }
  return keys.map((key) => {
    const lib = DIM_LIBS[key]
    const pool = pickPool(lib.items.length, POOL_SIZE[key], target[key])
    const items = pool.map((i) => at(lib.items, i))
    const targetName = at(lib.items, target[key]).name
    return {
      key,
      label: lib.label,
      badge: lib.badge,
      items,
      target: items.findIndex((it) => it.name === targetName),
    }
  })
}

/** 本地循环轮的随机 target（收敛轮的 target 由服务端下发，不用这里） */
export function randomTarget(): DressTarget {
  return {
    look: Math.floor(Math.random() * LOOKS.length),
    color: Math.floor(Math.random() * COLORS.length),
    waist: Math.floor(Math.random() * WAISTS.length),
    hair: Math.floor(Math.random() * HAIRS.length),
  }
}

// ---------- 稳定哈希（FNV-1a）：M3 服务端 dress_lock 就位前的客户端兜底 ----------

/** 字符串 → 32 位稳定哈希（同输入永远同输出，刷新/重进不漂移） */
export function stableHash(input: string): number {
  let hash = 0x811c9dc5
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return hash >>> 0
}

/** 当日动画方案：hash % 2（spec §1 variant 选择；M3 后由服务端覆盖） */
export function stableVariant(seed: string): 'dress' | 'sketch' {
  return stableHash(seed) % 2 === 0 ? 'dress' : 'sketch'
}

/** 确定性 target：收敛轮锁定目标（seed = 装机种子 + 日期，同人同天稳定） */
export function stableTarget(seed: string): DressTarget {
  const pick = (salt: string, len: number) => stableHash(`${seed}|${salt}`) % len
  return {
    look: pick('look', LOOKS.length),
    color: pick('color', COLORS.length),
    waist: pick('waist', WAISTS.length),
    hair: pick('hair', HAIRS.length),
  }
}

// ---------- dress_lock 语义值 → 洗牌库下标 ----------
// 服务端按语义值下发（variant.go 的词汇表与这里同名同序），换库顺序不改协议。

const LOOK_KEY_IDX: Record<string, number> = Object.fromEntries(LOOKS.map((l, i) => [l.key, i]))
const WAIST_NAME_IDX: Record<string, number> = Object.fromEntries(WAISTS.map((w, i) => [w.name, i]))
const COLOR_IDX_LOCK: Record<string, number> = Object.fromEntries(COLORS.map((c, i) => [c.name, i]))
const HAIR_IDX_LOCK: Record<string, number> = Object.fromEntries(HAIRS.map((h, i) => [h.key, i]))

/**
 * dress_lock.target 语义值 → DressTarget 下标。
 * 词汇表对不上（客户端落后/服务端新值）返回 null → 父组件回落旧揭晓线。
 */
export function dressTargetFromLock(lock: {
  look: string
  color: string
  waist: string
  hair: string
}): DressTarget | null {
  const look = LOOK_KEY_IDX[lock.look]
  const color = COLOR_IDX_LOCK[lock.color]
  const waist = WAIST_NAME_IDX[lock.waist]
  const hair = HAIR_IDX_LOCK[lock.hair]
  if (look === undefined || color === undefined || waist === undefined || hair === undefined) {
    return null
  }
  return { look, color, waist, hair }
}
