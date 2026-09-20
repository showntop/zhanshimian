# 每日内容 LLM 驱动重设计

日期：2026-09-20
分支：rebuild/daily-content
状态：已批准（产品定位 A / grounding A2 / 管线方案一 / 不留兼容层）

## 背景与决策

旧架构是 grounding-first 管线：22 条知识事实 → 规则选题（补最薄的格）→ LLM 只做文案改写 → refs 强制校验。暴露的问题：

1. 素材太薄：fact 是一句话格言，LLM 无可展开的机理与案例，内容天花板被锁死。
2. 选题坍缩：规则选题在 22 条事实/默认基因/无收藏用户上数学收敛到 color/fabric 两格（30 天复现：color×18、fabric×12、其余五格 0），跨天选题高度雷同。
3. LLM 只贡献措辞不贡献判断，「留住钩子」所需的新鲜感与个人相关度不足。

三项决策：

- **产品定位 A（留住钩子）**：新鲜感、个人相关度、当天语境优先；七格降级为事后归类。
- **grounding A2（检索增强）**：知识池不再强制引用，按相关性检索 4~6 条作为可选参考；正确性靠模型常识+参考纠偏，不做硬校验。
- **管线方案一（一次成文）**：选题与成文合并为单次 LLM 调用。
- **不留兼容层**：客户端与服务端同步改，旧协议字段直接删。

## §1 协议与管线

```
POST /v1/daily/prepare  → 纯缓存探测（一次 DB 读）：
    命中：{ gen_date, cache_hit: true, scenario: <当日 category> }
    未命中：{ gen_date, cache_hit: false, scenario: "" }
POST /v1/daily/generate → 单次 LLM 调用出全部内容：
    先查 TodayContent（幂等，重进秒回）→ 未命中才调 LLM
    请求体无 pick_token；响应 { source, content } 结构不变
```

- pickStore / pickSnapshot / pick_token 整套删除。
- prepare 保留仅因「重进不闪屏」：命中则客户端不播动画直接拉内容。

## §2 语境聚合与 prompt

LLM 输入（毫秒级 DB 读 + 一次天气查询，替代原规则选题）：

| 输入 | 来源 | 作用 |
|---|---|---|
| 日期/星期/季节 | genDate 推导 | 基本语境 |
| 天气 | amap provider | 当天贴身语境 |
| 用户画像 | ReadDailyGrounding → 基因（身高/骨架档位） | 个人相关度 |
| 近 14 天已推 topic+分类 | daily_content | 去重主机制 |
| 收藏信号 | collections 计数 + 近 5 条收藏 topic | 贴用户兴趣（七格逻辑反转） |
| 参考事实 4~6 条 | season+基因过滤后稳定抽样 | 检索增强，标注可选 |

输出 schema：topic/lead/fit/why/visual 不变；**category 由 LLM 自报**，取值
`{color, fit, proportion, fabric, occasion, howto, outfit, general}`；`refs` 删除。

模型路由不动（daily_content → qwen3.8-flash，json_object）。

## §3 校验与去重

- **保留**：禁则词、结构校验、visual 构建校验、失败带 hint 重试一次、兜底链（池+静态）。
- **删除**：refs 事实追溯、angle 轮换、dominantDomain/补最薄打分。
- **新增**：topic 去重闸——与近 14 天 topic 精确/规范化（去标点空白）比对，命中带 hint 重试一次，仍命中走兜底。不上 embedding。

## §4 数据与审计

- daily_content / knowledge_fact schema 不动。`fact_ids` 改存「本次提供的参考事实 ID」；`dedupe_key` 维持 `gen:日期:分类`。
- generation_run 保留；validation 删 `factTrace`，加 `dedup` 与 category 合法性。
- DAILY_FORCE_REGEN 语义不变（跳幂等+覆盖当天行）。

## §5 知识池运营（非代码）

fact 写法升级为 2~4 句有机理、适用条件、反例的短文；量级目标 100+ 条覆盖全季。

## §6 客户端

- `use-daily-pick.ts`：状态机不变；删 pick_token；scenario 播通用过场动画；cache_hit 分支不变。
- 手册容纳 `general`（「综合」），`dailyBucketName` 加映射。
- contracts/openapi.yaml 与生成客户端同步更新。

## 验收

- generation_run fallback 率显著下降。
- 同一用户 14 天内 topic 无重复。
- 跨天 category 分布不再坍缩在两格。
