-- 每日内容分格扩充（2026-09-21）：发型 / 妆容 / 配饰。
--
-- 手册从「七格 + general」扩为「十格 + general」（补齐形象顾问的三条主线）。
-- 本迁移只补内容资产，不改 schema：category 是 text 列，无枚举约束。
--   knowledge_fact  新格的参考事实（gene_fit 全为 '{}'＝不挑人：V1 的画像
--                   只有身高与骨架，声明条件的维度会匹配不上而落空）
--   daily_content   user_id IS NULL 的兜底池各补一条，保住「兜底尽量同格」
--
-- 判归规则（进 prompt 的同一份）：按决策变量归格——选什么（hair/makeup/
-- accessory） vs 怎么组合（outfit/proportion） vs 何时何地（occasion）。
-- 例：包「选什么体量」归 accessory，包「背在哪」归比例；发型为了修脸型，
-- 主体仍是头发，归 hair 不归 proportion。

-- domain 白名单随分格扩充：007 的 CHECK 约束只认七格，先换掉再插新事实。
-- （daily_content.category 无 CHECK，不用动。）
ALTER TABLE knowledge_fact DROP CONSTRAINT knowledge_fact_domain_check;
ALTER TABLE knowledge_fact ADD CONSTRAINT knowledge_fact_domain_check
  CHECK (domain IN ('color','fit','proportion','fabric','occasion','howto','outfit','hair','makeup','accessory'));

-- ---------- 知识事实 ----------
INSERT INTO knowledge_fact (domain, fact, boundary, gene_fit, season, source, reviewed) VALUES
  ('hair', '发型的量感要和五官的量感商量：贴头皮的直发把轮廓收干净，蓬松的卷度把存在感放大。', '五官存在感强的人两种都撑得住，此时要决定的是今天想强调什么。', '{}', '{}', '编辑整理', true),
  ('hair', '头发的长度改变的是脖颈与脸的可见比例：下巴附近的长度会截断颈部线条，锁骨以下或耳上很短，都把线条还给视线。', '颈偏长者不受此限，下巴长度也顺。', '{}', '{}', '编辑整理', true),
  ('hair', '换发型最便宜的第一步是换分界线：中分与侧分改变脸上的视觉重心，成本最低、反悔最快。', '发际线不齐的人优先侧分，不必硬拗中分。', '{}', '{}', '编辑整理', true),
  ('makeup', '妆容的重点只放一处：眼妆加重时唇色收中性，唇色加重时眼妆收干净，两处都重会互相抢。', '五官对比强的人本身焦点明确，两处都收淡反而没精神。', '{}', '{}', '编辑整理', true),
  ('makeup', '底妆的任务是均匀不是增白：色号在脸颊与脖颈的交界处不跳，整张脸的颜色系统才成立。', '肤色本身均匀时，底妆可以退到局部。', '{}', '{}', '编辑整理', true),
  ('makeup', '眉毛是表情的框架：眉尾落点抬一点，视觉重心上移；收平一点，气质更稳。改动可以只发生在眉尾一厘米里。', '自然眉形完整者，修去杂毛就够。', '{}', '{}', '编辑整理', true),
  ('accessory', '配饰的量感要与服装商量：强廓形的衣服会吞掉细小的首饰，解释度留给中等体量。', '极简的一身可以只留一件有分量的配饰做主角。', '{}', '{}', '编辑整理', true),
  ('accessory', '鞋是全身唯一承重的单品：鞋色与下装延续，视线落地不断线；鞋色一跳，全身的配色逻辑要重排。', '刻意用鞋做全场唯一主张色时，其余颜色都要收。', '{}', '{}', '编辑整理', true),
  ('accessory', '包的体量决定它的身份：装得下日常必需的最小号通常就是对的大小，大了压重心，小了沦为挂件。', '只为出席场合背的包，可以只讲比例不讲装载。', '{}', '{}', '编辑整理', true);

-- ---------- 兜底池（user_id IS NULL；source=fallback；每格一条保同格降级） ----------
INSERT INTO daily_content (user_id, gen_date, category, topic, lead, fit_text, why, visual, fact_ids, source, model_key, dedupe_key) VALUES
  (NULL, '1970-01-01', 'hair', '分界线决定视觉重心', '换发型不必先动剪刀，先动分界线。', '中分与侧分改变的是脸上的视觉重心：从现在的分界线往侧边移一指，重心就跟着走，成本最低、反悔最快。', '分界线是最便宜的发型改动，它决定视线先落在哪。', '{"modality":"compare","spec":{"left":{"label":"中分 · 居中","tone":"#8A8F83"},"right":{"label":"侧分 · 偏移","tone":"#8E9A83","layered":true},"marker":"分界线"},"alt":"中分与侧分的对比，侧分把视觉重心带离正中"}', '{}', 'fallback', '', 'hair.parting.weight'),
  (NULL, '1970-01-01', 'makeup', '妆容只放一个重点', '眼妆和唇色，今天选一处。', '先定今天的主角：眼妆加重时唇色收中性，唇色加重时眼妆收干净——两处都重会互相抢。', '一个焦点才成立，两个强点会互相抵消。', '{"modality":"swatch","spec":{"items":[{"tone":"#8E9A83","label":"主角 · 加重","state":"pick"},{"tone":"#C9CFD4","label":"另一处 · 收住"},{"tone":"#5A6156","label":"底妆 · 贴肤"}]},"alt":"妆容配比：一处主角加重，其余收住"}', '{}', 'fallback', '', 'makeup.one.accent'),
  (NULL, '1970-01-01', 'accessory', '鞋与下装的颜色连续', '视线落地不断线，比例就顺了。', '鞋的颜色往裤色或裙色上靠，是全身最省力的一种连续；鞋色一跳，整身的配色逻辑要重新排。', '鞋是全身唯一承重的单品，它的颜色决定视线怎么落地。', '{"modality":"compare","spec":{"left":{"label":"鞋与下装同色","tone":"#8E9A83"},"right":{"label":"鞋色跳开","tone":"#5A6156"},"marker":"落地点"},"alt":"鞋与下装同色延续和鞋色跳开的对比"}', '{}', 'fallback', '', 'accessory.shoe.continuity');
