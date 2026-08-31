# 怎么打扮微信小程序

这是原生微信小程序工程。用微信开发者工具直接导入 `miniapp/`，开发模式 API 默认是 `http://127.0.0.1:58000`，`project.config.json` 已关闭本地域名校验。

首次体验：

1. 启动根目录 `docker compose --env-file server/.env up --build`。
2. 在微信开发者工具导入此目录。
3. 首页点击“开始形象分析”。
4. 在照片页点击“使用示例照片体验”，即可跑通报告、三套方案、完整方案、改造清单和反馈。

开发版默认请求 `http://127.0.0.1:58000`。体验版和正式版不会回退到本地地址：发布前必须在 `config/runtime.js` 分别填写 `trial`/`release` HTTPS API 域名（第三方平台代开发也可通过 `extConfig.apiBaseURL` 注入），并在微信公众平台配置 request/uploadFile 合法域名。未配置时客户端会明确报错，不会尝试开发登录。

微信 3.17+ 的 `<image>` 不再渲染 `http://` 图片。本地开发时 API 返回的用户照片是 http 链接，`services/api.js` 会把响应里的 http 图片 URL 经 `wx.downloadFile` 换成本地临时路径后再交给页面；体验版/正式版全是 https，不会触发该逻辑。

页面结构：

- `home`：新用户/老用户双状态顾问工作台、快捷工具与场景入口
- `scene`：时间、预算、正式程度和目标印象组成的单页轻问卷
- `hair`：发型实图预览、方案切换与保存
- `outfit`：今日穿搭上传、问题标注与优先调整建议
- `purchase`：商品截图上传、适配判断与搭配建议
- `lab`：发型/妆容 AR、3D 形象 Lite 和上半身试衣的实验入口
- `capture`：三张照片采集与隐私说明
- `analysis`：异步分析进度
- `report`：当前形象、Tag 和优先建议
- `plans`：三套本人方案轮播与前后对比
- `plan`：发型、妆容、穿搭详细方案
- `checklist`：可勾选改造清单
- `feedback`：实际体验反馈闭环
- `profile`：档案、选填身体数据、实验能力和数据删除
