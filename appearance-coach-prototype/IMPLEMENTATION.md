# 完整实现说明

- `src/`：此前的 React 手机视觉原型，保留作为设计参考。
- `miniapp/`：正式原生微信小程序实现。
- `server/`：Go API、分析 Worker、Demo/OpenAI 视觉分析 Provider、PostgreSQL Repository。
- `docs/technical-design.md`：详细架构、选型、API、数据、安全和部署方案。
- `docker-compose.yml`：本地 PostgreSQL 与 API。

默认 Demo Provider 不会对用户做颜值评分，也不会声称医学或确定性结论；它用于稳定验证产品闭环。OpenAI Provider 已完成三图读取、Responses API 图像分析、严格 JSON Schema、二次安全校验、超时控制和可配置 Demo 回退。当前方案人物图仍是明确标记的静态效果示例，不冒充针对用户生成的真实换装结果。

## APP 化产品结构

- 首页采用首次使用与回访双状态，不使用落地页式超大标题或长营销文案。
- `开始形象分析` 保持独立主入口；面试、婚礼、约会、日常统一进入单页场合需求，不走多步问卷。
- 回访首页提供发型预览、穿搭诊断、购买判断三个任务型入口，并展示最近方案与今日建议。
- 底部导航固定为 `首页 / 方案 / 我的`，不增加商城、社区或发现入口。
- 发型 AR、3D 形象 Lite 与上半身试衣放在体验实验室；体重与三围保持选填，不阻塞基础分析。

## 已实现闭环

`首页 → 三图建档 → 异步分析 → 当前形象报告 → 三套方案 → 方案详情/前后对比 → 改造清单 → 实拍反馈 → 档案复用`

- 首页与建档页按已确认视觉稿重构；面试、婚礼、约会、日常是独立快捷入口。
- 已有形象档案时，场景入口直接复用报告进入方案，不重复收集三张照片。
- 建档支持连续调用相机、批量从相册选择、单槽重拍、拍摄帮助和示例体验。
- 报告、三方案、完整方案、清单、反馈、档案均连接真实 Go API；加载、空态、失败重试和数据删除均有对应状态。
- 发型预览、穿搭诊断和购买判断均使用鉴权 API；结构化结论、问题标签、推荐选项与保存状态写入 PostgreSQL，可关联当前档案与上传照片。
- 设置 `AI_PROVIDER=openai` 后，基础报告与三套文字方案来自真实照片分析；`/healthz` 可确认当前 Provider 和回退状态，报告的 `provider_version` 可追溯实际产出来源。
- 发型预览已升级为独立异步闭环：正脸照上传、方向选择、生成进度、原图/效果对比、Provider 标识和结果保存。`HAIR_PREVIEW_PROVIDER=openai` 时使用 GPT Image 2 图像编辑；默认 Demo 结果明确标为效果示例。
- 穿搭诊断已接入可配置的真实单图视觉 Provider：照片权限校验、严格结构化三处标注、最高优先级建议、低成本调整方案和 Provider 来源标识均已完成。

## 验证命令

```bash
node miniapp/scripts/validate.mjs
npm run build
npm run test:sites
MOBILE_RUNTIME_TEST_PORT=4181 npm run test:runtime -- --reporter=line
cd server && go test ./... && go vet ./...
cd ..
bash server/scripts/e2e.sh
```
