# AI 能力路由与模型接入

## 目标

业务代码只依赖能力，不依赖厂商或模型。更换 Qwen、Wan、Seedream 或 OpenAI 时，只调整 `server/config/ai-routing.*.json`；照片读取、业务 Schema、输出安全校验和 COS 持久化仍由应用控制。

```text
形象分析 / 穿搭诊断 / 购买判断 / 顾问 / 发型预览
                         │
                         ▼
                 Capability Router
              主模型 → 同能力真实备用模型
                         │
            ┌────────────┴────────────┐
            ▼                         ▼
   Structured Generation         Image Editing
   OpenAI Chat/Responses      Wan / Ark / OpenAI Edit
            │                         │
            └────────────┬────────────┘
                         ▼
             业务校验 + 自有 COS 持久化
```

## 配置结构

- `models` 是部署级模型目录：厂商、协议、模型 ID、Base URL、密钥环境变量名、超时、结构化模式和估算单价。
- `routes` 是能力路由：主模型、真实备用模型、质量/性价比策略标签和单次预算上限。
- `api_key_env` 只保存环境变量名。真实 Secret 永远不进入配置文件和 Git。
- `structured_mode=json_schema` 用于支持严格 Schema 的模型；`json_object` 用于更便宜的模型，运行时会把 Schema 注入提示词并由领域层再次校验。

## 切换模型

同协议切换只改路由的 `primary`。跨厂商切换时，在 `models` 增加一项并选用已有协议；业务代码不动。只有接入一种全新的网络协议时，才新增一个 Runtime adapter，并用合约测试覆盖鉴权、请求形状、响应解析、超时、图片大小和 MIME 校验。

## 上线原则

1. 形象分析以效果为先：固定评测集胜出后才能改主路由。
2. 顾问对话和简单诊断以性价比为先，但服务端 Schema/安全校验不能降低。
3. 本人预览属于图片编辑，不用纯文生图模型；必须在提示词中锁定身份、姿势、服装、背景和镜头。
4. 同厂商备用解决模型故障，跨厂商备用解决区域或供应商故障；至少保留一个跨厂商图片回退。
5. `max_cost_cny` 在请求前按模型标价与输出预算拦截。调用后的真实 usage 用于日志与后续成本看板，不以“已花费的请求被拒绝再回退”的方式控费。

## 当前边界

`hair_edit` 已有完整业务入口。`makeup_edit` 和 `full_look_edit` 已纳入同一模型目录和图片协议，但产品入口仍应在身份一致性评测通过后逐步开放。模型路由解决的是供应商切换与能力治理，不等于 3D 建模或实时 AR 试衣。

## 官方接口依据

- [百炼 OpenAI Chat Completions 兼容接口](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)
- [百炼千问结构化输出](https://help.aliyun.com/zh/model-studio/qwen-structured-output)
- [万相 2.7 图片生成与编辑 API](https://help.aliyun.com/zh/model-studio/wan-image-generation-and-editing-api-reference)
- [火山引擎 Seedream 图片生成](https://www.volcengine.com/docs/6492/2221472?lang=zh)
