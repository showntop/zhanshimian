# 见我 API

Go 1.24 + PostgreSQL 16。API 启动时自动执行向前迁移，开发模式在 API 进程内运行分析 Worker。

除三图分析、报告、方案、清单和反馈外，API 也提供异步本人发型预览、真实穿搭诊断、购买判断及结果保存接口。默认 Demo Provider 返回稳定示例；配置 OpenAI Provider 后，系统会读取用户照片并生成结构化报告、发型编辑图和单图穿搭建议。

```bash
cp server/.env.example server/.env
docker compose --env-file server/.env up -d --build
curl http://127.0.0.1:58000/healthz
```

Compose 会从 `server/.env` 加载 API 环境变量；`server/.env` 不应提交到 Git。

健康接口同时返回当前分析模式与是否允许 Demo 回退：

```json
{"data":{"status":"ok","analysis_provider":"demo","hair_preview_provider":"demo","outfit_diagnosis_provider":"demo","fallback_enabled":false}}
```

本地不使用 Docker 运行 API：

```bash
export ADDR=':58000'
export DATABASE_URL='postgres://jianwo:jianwo@localhost:55432/jianwo?sslmode=disable'
go run ./cmd/api
```

启用真实三图分析：

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

OpenAI 分析与穿搭 Provider 使用 Responses API 的图像输入与严格 JSON Schema 输出，并在模型响应后再次执行服务端字段、枚举、数量和安全措辞校验。发型 Provider 使用异步图像编辑任务，生成文件经格式/大小校验后写入对象存储。三个回退开关适合演示环境保持流程可用；生产评估与灰度阶段应设为 `false`，避免将降级示例误当作真实结果。分析请求使用 `store: false`，但应用自身的照片保存与删除策略仍需按隐私条款执行。

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
