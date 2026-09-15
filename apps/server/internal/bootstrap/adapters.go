package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	identitypayment "github.com/zhanshimian/server/internal/provider/payment"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/account"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/body"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/storage"
)

// mediaPresenter 把 MediaAsset 投影成可渲染媒体：签 URL + 来源/角标映射。
// COS 走短时签名；本地存储没有签名能力时回退 PUBLIC_BASE_URL + /uploads/
// 公开路径（与 bodyURLSigner 同一做法，开发环境可播）。
type mediaPresenter struct {
	signer        storage.SignedURLStorage
	ttl           time.Duration
	publicBaseURL string
}

// newMediaPresenter 为读路径装配媒体呈现器：支持签名的存储走签名，
// 否则回退公开前缀拼接。
func newMediaPresenter(objects storage.ObjectStorage, cfg config.Config) mediaPresenter {
	var signer storage.SignedURLStorage
	if s, ok := objects.(storage.SignedURLStorage); ok {
		signer = s
	}
	return mediaPresenter{signer: signer, ttl: cfg.AssetURLTTL, publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/")}
}

func (p mediaPresenter) Present(ctx context.Context, asset domain.MediaAsset) (assessment.PresentedMedia, error) {
	presented := assessment.PresentedMedia{
		MIMEType:     asset.MIMEType,
		SourceKind:   string(domain.SourceKindOf(asset.Origin)),
		DisplayLabel: domain.DisplayLabelOf(asset.DisplayKind),
	}
	if p.signer != nil {
		url, err := p.signer.SignedURL(ctx, asset.ObjectKey, p.ttl)
		if err != nil {
			return presented, err
		}
		presented.URL = url
		presented.URLExpiresAt = time.Now().Add(p.ttl).UTC()
	} else if p.publicBaseURL != "" {
		presented.URL = p.publicBaseURL + "/uploads/" + strings.TrimPrefix(asset.ObjectKey, "/")
		presented.URLExpiresAt = time.Now().Add(p.ttl).UTC()
	}
	return presented, nil
}

// imageLoader 从对象库读照片字节，供 assessment worker 提取图片。
// 优先走数据万象下载时压缩（未开通 CI 自动回退原图），再用本地预算
// 约束兜底，避免原图 base64 撑爆 AI 请求体。
type imageLoader struct {
	objects storage.ObjectStorage
}

func (l imageLoader) Load(ctx context.Context, items []domain.PhotoSetItem) ([]assessment.ImageInput, error) {
	images := make([]assessment.ImageInput, 0, len(items))
	for _, item := range items {
		rc, err := storage.OpenProcessedOr(ctx, l.objects, item.Asset.ObjectKey, providerai.VisionCOSProcess)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", item.Asset.ObjectKey, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		data, mime := providerai.ConstrainVisionImage(data, item.Asset.MIMEType)
		images = append(images, assessment.ImageInput{Role: string(item.Role), MIMEType: mime, Data: data})
	}
	return images, nil
}

// operationProgress 把 Store 的进度上报适配到 assessment.OperationProgress。
type operationProgress struct {
	store *postgres.Store
}

func (p operationProgress) Set(ctx context.Context, operationID string, progressBPS int, stageCode string) error {
	return p.store.SetOperationProgress(ctx, operationID, progressBPS, stageCode)
}

// todayWeatherAdapter 把 provider.WeatherProvider 适配到 today.WeatherProvider。
type todayWeatherAdapter struct{ inner provider.WeatherProvider }

func (a todayWeatherAdapter) Current(ctx context.Context, city string) (today.Weather, error) {
	w, err := a.inner.Current(ctx, city)
	return today.Weather{City: w.City, Condition: w.Condition, Temperature: w.Temperature}, err
}

// mediaURLSigner 给外围读模型（home/today/diagnostic/wardrobe）的展示媒体
// 解析可读 URL：COS 走短时签名；本地存储没有签名能力时回退
// PUBLIC_BASE_URL + /uploads/ 公开路径（与 mediaPresenter/bodyURLSigner
// 同一做法，开发环境可播）。url_expires_at 语义是「此刻起 ttl 内新鲜」，
// 公开 URL 实际不过期。
type mediaURLSigner struct {
	signer        storage.SignedURLStorage
	ttl           time.Duration
	publicBaseURL string
}

func newMediaURLSigner(objects storage.ObjectStorage, publicBaseURL string, ttl time.Duration) mediaURLSigner {
	var signer storage.SignedURLStorage
	if s, ok := objects.(storage.SignedURLStorage); ok {
		signer = s
	}
	return mediaURLSigner{signer: signer, ttl: ttl, publicBaseURL: strings.TrimRight(publicBaseURL, "/")}
}

func (s mediaURLSigner) SignedURL(ctx context.Context, objectKey string) (string, time.Time, error) {
	expiresAt := time.Now().Add(s.ttl).UTC()
	if s.signer != nil {
		url, err := s.signer.SignedURL(ctx, objectKey, s.ttl)
		if err != nil {
			return "", time.Time{}, err
		}
		return url, expiresAt, nil
	}
	return s.publicBaseURL + "/uploads/" + strings.TrimPrefix(objectKey, "/"), expiresAt, nil
}

// shareURLSigner 把 mediaURLSigner 适配到 share.URLSigner（ttl 构造时固定，
// 与读模型签名同一策略；本地存储同样回退公开路径）。
type shareURLSigner struct{ inner mediaURLSigner }

func (s shareURLSigner) SignedURL(ctx context.Context, objectKey string, _ time.Duration) (string, time.Time, error) {
	return s.inner.SignedURL(ctx, objectKey)
}

// avatarURLResolver 把头像 object key 解析为可加载 URL：
// 支持签名的存储走短时签名，否则回退公网前缀拼接。
type avatarURLResolver struct {
	objects       storage.ObjectStorage
	publicBaseURL string
	ttl           time.Duration
}

func (r avatarURLResolver) ResolveAssetURL(objectKey string) string {
	if signer, ok := r.objects.(storage.SignedURLStorage); ok {
		if signed, err := signer.SignedURL(context.Background(), objectKey, r.ttl); err == nil {
			return signed
		}
	}
	return r.publicBaseURL + "/" + strings.TrimPrefix(objectKey, "/")
}

// billingSessionExchanger 把 identity 的会话换发适配到 billing 所需形状。
type billingSessionExchanger struct {
	inner provider.WeChatSessionExchanger
}

func (a billingSessionExchanger) ExchangeSession(ctx context.Context, code string) (identitypayment.WeChatSession, error) {
	session, err := a.inner.ExchangeSession(ctx, code)
	return identitypayment.WeChatSession{OpenID: session.OpenID, SessionKey: session.SessionKey}, err
}

// deleteObjectAdapter 把 storage.Delete 包成回调供 account 删除数据后回收对象。
type deleteObjectAdapter struct{ objects storage.ObjectStorage }

func (a deleteObjectAdapter) Delete(key string) error {
	return a.objects.Delete(context.Background(), key)
}

// demoMediaAdapter 提供 Demo 媒体（POST /v1/media/demo）：
// 内置 assets/looks/*.png 写入对象存储（worker 与签名 URL 都按真实对象取），
// 再落 origin=demo 的媒体行，展示侧映射为 效果示例；创建后即刻呈现为
// DisplayMedia（含签名/回退 URL），满足契约 createDemoMedia 响应。
type demoMediaAdapter struct {
	store     *postgres.Store
	objects   storage.ObjectStorage
	assetDir  string
	presenter mediaPresenter
}

var demoKinds = map[string]bool{"face": true, "side": true, "body": true, "outfit": true, "product": true, "wardrobe": true}

// demoBundledAsset 决定每种 demo 用哪张内置图：三图与穿搭诊断用与拍摄引导
// 同源的真人示例（assets/demo/*.jpg，能过内容门禁）；单品/衣橱沿用 looks 渲染图。
func demoBundledAsset(kind string) (file, ext, mime string) {
	switch kind {
	case "face", "side", "body", "outfit":
		return filepath.Join("demo", kind+".jpg"), "jpg", "image/jpeg"
	case "product":
		return filepath.Join("looks", "warm.png"), "png", "image/png"
	default: // wardrobe
		return filepath.Join("looks", "natural.png"), "png", "image/png"
	}
}

func (a demoMediaAdapter) CreateDemoMedia(ctx context.Context, userID, kind string) (assessment.PresentedMedia, error) {
	if !demoKinds[kind] {
		return assessment.PresentedMedia{}, fmt.Errorf("%w: unsupported photo kind", account.ErrValidation)
	}
	bundled, ext, mime := demoBundledAsset(kind)
	file, err := os.Open(filepath.Join(a.assetDir, bundled))
	if err != nil {
		return assessment.PresentedMedia{}, fmt.Errorf("open bundled demo asset: %w", err)
	}
	defer file.Close()

	objectKey := fmt.Sprintf("demo/%s/%s-%s.%s", userID, kind, uuid.NewString(), ext)
	sum := sha256.New()
	counter := &countingReader{reader: io.TeeReader(file, sum)}
	if _, err := a.objects.Save(ctx, objectKey, counter); err != nil {
		return assessment.PresentedMedia{}, fmt.Errorf("save demo object: %w", err)
	}
	asset, err := a.store.InsertDemoMedia(ctx, userID, kind, objectKey, hex.EncodeToString(sum.Sum(nil)), counter.n, mime)
	if err != nil {
		return assessment.PresentedMedia{}, err
	}
	presented, err := a.presenter.Present(ctx, asset)
	if err != nil {
		return assessment.PresentedMedia{}, err
	}
	presented.AssetID = asset.ID
	return presented, nil
}

type countingReader struct {
	reader io.Reader
	n      int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.n += int64(n)
	return n, err
}

// eventWriterAdapter 把 Store 的埋点行写入适配到 httpapi.EventWriter。
type eventWriterAdapter struct{ store *postgres.Store }

func (a eventWriterAdapter) TrackProductEvent(ctx context.Context, userID string, input domain.ProductEventInput) error {
	return a.store.TrackProductEventRow(ctx, userID, input)
}

// bodySigner 给 3D 形象 Lite 的读取投影选签名器：COS 走签名 URL，本地
// 存储退化为 PUBLIC_BASE_URL + /uploads/（开发环境可播）。
func bodySigner(objects storage.ObjectStorage, cfg config.Config) body.URLSigner {
	var signer storage.SignedURLStorage
	if s, ok := objects.(storage.SignedURLStorage); ok {
		signer = s
	}
	return bodyURLSigner{signer: signer, ttl: cfg.AssetURLTTL, publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/")}
}
