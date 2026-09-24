-- 2026-09-24 每日内容携带洗牌定格参数（spec 2026-09-23-dress-shuffle 后续）。
--
-- lock 与建议同源：生成建议的同一次 LLM 调用产出的定格参数（look/color/waist/hair，
-- 语义值，词表在服务端 service/daily 与客户端 presets.ts），随 daily_content 落库，
-- dress_lock 收敛脚本下发时优先使用。NULL = 该条没有定格参数（兜底池条目 /
-- 旧数据），脚本按既有逐维回退（category 就近归类 + hash(uid+date) 稳定随机）。
--
-- 归属由外键结构性保证：注销（DELETE /v1/me/data）经 users 级联带走，
-- 不需要单独的清除分支。

ALTER TABLE daily_content ADD COLUMN lock jsonb;
