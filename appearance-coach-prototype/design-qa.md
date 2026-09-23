# 首页 UI 修复：Design QA

## Comparison target

- Source visual truth: 用户提供的截图 `/var/folders/jn/1zg6fv2x4p5d_ynwx4p2g4pm0000gn/T/codex-clipboard-62a5fb0f-fdd4-487f-ad04-83b0566c46df.png`（634 × 1172 px）；本轮缺陷截图为 `/var/folders/jn/1zg6fv2x4p5d_ynwx4p2g4pm0000gn/T/codex-clipboard-3e5e3a0a-0863-4c2c-81fb-84ace006d208.png`。
- Implementation: 微信开发者工具中 `jianwo-miniapp` 的 `pages/home/index`，本轮复核截图为 `/private/tmp/home-bottom-native-scroll.png`。
- Side-by-side evidence: `/private/tmp/home-bottom-before-native-scroll.png`（1187 × 1160 px）。两张设备截图都等比归一到 1160 px 高，完整保留底部导航与「最近方案」卡片，便于判断遮挡情况。
- Route/state: `pages/home/index`，已建立形象档案的回访用户；有「今日完整方案」、3 个处理中任务、4 个场景入口和最近方案。
- Simulator: WeChat DevTools，iPhone 15 Pro Max，61% 画布缩放。完整首页和缩略图区域均已做对照。

## Comparison history

### Iteration 1 — source issues

- [P1] 象牙白页面上的系统状态栏为白色，时间、Wi‑Fi 和电量几乎不可见。
- [P1] 3 条任务进度纵向堆叠，挤压了首屏，最近方案卡片容易落到原生 TabBar 下方。
- [P1] 小尺寸「风格参考」缩略图使用过强的模糊，人物内容近似灰块，角标还会折行。
- [P2] 首页 WXSS 使用了开发者工具不支持的标签/伪类选择器，编译控制台留下源样式告警。

### Iteration 2 — implemented fixes

- 将页面状态栏前景设为深色，并保持暖白背景，恢复系统信息可读性。
- 为自定义滚动页预留原生 TabBar 与安全区高度；将任务改为横向可滑动轨道，首屏保留任务状态并露出下一张卡片提示可继续查看。
- 仅在首页缩略图将示例弱化从 `blur(4px)` 降至 `blur(1.2px)`，保留「风格参考」语义；同时缩小角标、禁止换行。
- 将不兼容的 WXSS 写法替换为明确的类选择器。清空并重新编译后，控制台只剩开发者工具自动热重载提示，没有首页源码 WXSS 告警。

### Iteration 3 — bottom clearance

- [P1 fixed] 根因是首页另套了一层定高 `scroll-view`，却又使用 `100vh`/TabBar/安全区估算可用高度。原生 TabBar 已有自己的窗口占位，这两套高度体系叠加后形成了截图中白色的遮挡区域。
- 移除首页的内层定高 `scroll-view`，改回小程序原生页面滚动；内容区域现在由 WeChat 按实际 TabBar 与安全区计算。
- 恢复「最近方案」原有的图片高度、顶部间距和卡片底栏。此前为规避遮挡而压缩它的做法已撤销。
- 重新向下滚动复核：完整对比卡片、`查看 ›` 入口和快捷工具都可停在原生 TabBar 之上，没有任何内容落在导航区域内。

## Final visual review

- **Typography:** 保持现有 PingFang 层级；状态栏、任务标题/说明、卡片 CTA 与章节标题均清晰可读，未见截断或异常换行。
- **Layout:** 今日建议仍是首要内容；任务区由高占用纵向列表压缩为一行横向轨道。最近方案的图片、标题和入口均在原生底部导航上方完成，向下滚动后快捷工具也不会被导航覆盖。
- **Color:** 延续暖白、鼠尾草与苔藓绿；状态栏以黑色提供必要对比，不改变主视觉色调。
- **Imagery:** 参考图现在能辨识人物、室内背景和蓝色服装，同时仍以轻微弱化与角标区别于用户真实照片。
- **Copy:** 保留既有中文内容、天气和方案层级；没有为修复而改写业务文案。
- **Accessibility:** 系统状态信息恢复可见；任务横滑区有连续卡片露出，降低横向可发现性问题；所有可点击图片与任务仍保留原有事件绑定。

## Functional checks

- `node miniapp/scripts/validate.mjs`：通过，18 个页面校验成功。
- `node --check miniapp/pages/home/index.js`：通过。
- `npm run check:runtime`：通过，28 个受保护运行时文件完整。
- `git diff --check`：通过，无空白错误。
- 微信开发者工具：首页成功热重载；点击参考缩略图可打开并关闭大图；任务横向轨道、最近方案底部入口和原生底部导航均按预期显示。

## Remaining P3 / test gap

- 本次在 iPhone 15 Pro Max 模拟器完成视觉复核；建议上线前再用真机确认安全区和横向滑动的手势手感。

## Final result

final result: passed
