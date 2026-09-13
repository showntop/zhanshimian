package bootstrap

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	identitypayment "github.com/zhanshimian/server/internal/provider/payment"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/account"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/storage"
)

// mediaPresenter 把 MediaAsset 投影成可渲染媒体：签 URL + 来源/角标映射。
type mediaPresenter struct {
	signer storage.SignedURLStorage
	ttl    time.Duration
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
	}
	return presented, nil
}

// imageLoader 从对象库读照片字节，供 assessment worker 提取图片。
type imageLoader struct {
	objects storage.ObjectStorage
}

func (l imageLoader) Load(ctx context.Context, items []domain.PhotoSetItem) ([]assessment.ImageInput, error) {
	images := make([]assessment.ImageInput, 0, len(items))
	for _, item := range items {
		rc, err := l.objects.Open(ctx, item.Asset.ObjectKey)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", item.Asset.ObjectKey, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		images = append(images, assessment.ImageInput{Role: string(item.Role), MIMEType: item.Asset.MIMEType, Data: data})
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

// signedURLSigner 把 storage 的双返回值签名适配到 share.URLSigner。
type signedURLSigner struct{ inner storage.SignedURLStorage }

func (s signedURLSigner) SignedURL(ctx context.Context, objectKey string, ttl time.Duration) (string, time.Time, error) {
	url, err := s.inner.SignedURL(ctx, objectKey, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return url, time.Now().Add(ttl).UTC(), nil
}

// hairStarterAdapter 把 Store 的 hair 预览 Operation 创建适配到 hair.OperationStarter。
type hairStarterAdapter struct{ store *postgres.Store }

func (a hairStarterAdapter) StartPreviewOperation(ctx context.Context, userID string, previewID string) (domain.OperationRef, error) {
	return a.store.StartPreviewOperation(ctx, userID, previewID)
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

// demoMediaAdapter 提供 Demo 媒体行（POST /v1/media/demo）：
// 与 legacy CreateDemoMedia 同一实现——内置 demo/<kind>.png，.origin=demo。
type demoMediaAdapter struct{ store *postgres.Store }

var demoKinds = map[string]bool{"face": true, "side": true, "body": true, "outfit": true, "product": true, "wardrobe": true}

func (a demoMediaAdapter) CreateDemoMedia(ctx context.Context, userID, kind string) (domain.MediaAsset, error) {
	if !demoKinds[kind] {
		return domain.MediaAsset{}, fmt.Errorf("%w: unsupported photo kind", account.ErrValidation)
	}
	return a.store.CreateMedia(ctx, userID, kind, "demo/"+kind+".png", "image/png", 1)
}

// eventWriterAdapter 把 Store 的埋点行写入适配到 httpapi.EventWriter。
type eventWriterAdapter struct{ store *postgres.Store }

func (a eventWriterAdapter) TrackProductEvent(ctx context.Context, userID string, input domain.ProductEventInput) error {
	return a.store.TrackProductEventRow(ctx, userID, input)
}
