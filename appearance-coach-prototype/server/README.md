# 怎么打扮 API

Go 1.24 + PostgreSQL 16。API 启动时自动执行向前迁移，开发模式在 API 进程内运行分析 Worker。

除三图分析、报告、方案、清单和反馈外，API 也提供异步本人发型预览、真实穿搭诊断、真实购买判断、档案增强顾问对话及结果保存接口。默认 Demo Provider 返回稳定示例；配置统一 AI 能力路由后，系统会读取用户照片并生成结构化报告、本人发型编辑图与单图诊断。

```bash
cp server/.env.example server/.env
docker compose --env-file server/.env up -d --build
curl http://127.0.0.1:58000/healthz
```

Compose 会从 `server/.env` 加载 API 环境变量；`server/.env` 不应提交到 Git。

健康接口同时返回当前分析模式和每项能力实际选择的模型：

```json
{"data":{"status":"ok","ai_routes":{"appearance_analysis":"aliyun/qwen3.7-plus","hair_edit":"aliyun/wan2.7-image-pro"}}}
```

本地不使用 Docker 运行 API：

```bash
export ADDR=':58000'
export DATABASE_URL='postgres://jianwo:jianwo@localhost:55432/jianwo?sslmode=disable'
go run ./cmd/api
```

## 启用统一真实 AI 路由（推荐）

复制模型目录后只需替换百炼 Workspace ID；密钥继续放环境变量，不能写进 JSON：

```bash
cp server/config/ai-routing.example.json server/config/ai-routing.local.json
# 编辑 ai-routing.local.json 中的 YOUR_WORKSPACE_ID
export AI_ROUTING_FILE="$PWD/server/config/ai-routing.local.json"
export ALIYUN_API_KEY='...'
export VOLCENGINE_API_KEY='...'
go run ./cmd/api
```

Docker Compose 使用：

```bash
AI_ROUTING_FILE=/app/config/ai-routing.json
AI_ROUTING_HOST_FILE=./server/config/ai-routing.production.json
ALIYUN_API_KEY=...
VOLCENGINE_API_KEY=...
```

路由配置把业务能力与模型解耦。目前支持 `appearance_analysis`、`outfit_diagnosis`、`purchase_diagnosis`、`advisor_chat`、`hair_edit`、`makeup_edit` 和 `full_look_edit`；协议适配器支持 OpenAI Responses、OpenAI Chat Completions、OpenAI Images Edit、阿里万相与火山 Ark 图片生成。主备模型、超时、成本单价和单次预算都在同一个目录内调整，业务 Service 不需要改动。

推荐的默认策略是：三图分析用 Qwen3.7 Plus 严格 JSON Schema；低风险诊断/顾问用 Flash 的 JSON Object 后再做服务端校验；本人发型图优先 Wan2.7 Image Pro，Seedream 4.5 作跨厂商故障回退。万相返回的临时图片会立即下载、校验并写入应用自己的 COS，不直接把供应商临时 URL 返回给小程序。

旧版单一 OpenAI 配置仍可用于本地兼容；生产启动门禁要求统一路由，防止购买判断、顾问或故障回退仍落到静态 Demo：

```bash
export AI_PROVIDER=openai
export OPENAI_API_KEY='...'
export OPENAI_VISION_MODEL=gpt-5-mini
export HAIR_PREVIEW_PROVIDER=openai
export OPENAI_IMAGE_MODEL=gpt-image-2
export OPENAI_IMAGE_QUALITY=medium
export OUTFIT_DIAGNOSIS_PROVIDER=openai
export OPENAI_OUTFIT_MODEL=gpt-5-mini
export AI_FALLBACK_TO_DEMO=false
export HAIR_PREVIEW_FALLBACK_TO_DEMO=false
export OUTFIT_DIAGNOSIS_FALLBACK_TO_DEMO=false
go run ./cmd/api
```

所有结构化输出在模型约束后还会执行服务端字段、枚举、数量与安全措辞校验。路由失败只会尝试配置的真实备用模型；生产环境禁止悄悄退回 Demo，避免把示例冒充本人结果。每次调用以结构化日志记录 capability、实际模型、请求 ID、耗时和估算费用，但不记录照片 Base64、API Key 或原始模型响应。应用自身的照片保存与删除策略仍需按隐私条款执行。

## 发布基础配置

正式微信登录、腾讯云 COS 私有对象读写/预签名 URL 和高德实时天气均已提供适配器。生产环境设置 `APP_ENV=production` 后会执行启动门禁：开发登录必须关闭，API/COS 必须使用非本机 HTTPS 地址，微信密钥、COS 密钥和高德 Web 服务 Key 必须齐全，存储和天气 Provider 不允许继续使用 Demo。

```bash
APP_ENV=production
PUBLIC_BASE_URL=https://prompt.wuyill.com/zhanshimian
DEV_LOGIN_ENABLED=false
WECHAT_APP_ID=wx911e0fbcba0b24d0
WECHAT_APP_SECRET=...
STORAGE_PROVIDER=cos
ASSET_BUCKET=wuyill-1252214184
ASSET_S3_ENDPOINT=https://cos.ap-beijing.myqcloud.com
ASSET_REGION=ap-beijing
COS_SECRET_ID=...
COS_SECRET_KEY=...
COS_KEY_PREFIX=zhanshimian/production
WEATHER_PROVIDER=amap
AMAP_WEB_SERVICE_KEY=...
```

如果直接使用 Docker Compose，可从 `server/.env.production.example` 复制。`ASSET_BUCKET + ASSET_S3_ENDPOINT` 会自动转换为 COS SDK 所需的 `COS_BUCKET_URL`；也可以直接填写完整 `COS_BUCKET_URL` 覆盖自动推导。

COS 桶应保持私有；服务端只返回默认 15 分钟有效的 GET 预签名 URL。COS 子账号只授予目标前缀的 Put/Get/DeleteObject 权限，密钥不得提交仓库。微信 AppSecret 与高德 Key 同样只通过部署平台 Secret 注入。正式放量前还需按 [`docs/ai-evaluation.md`](../docs/ai-evaluation.md) 建立固定评测集和人工复核门槛。
