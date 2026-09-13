package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/httpapi"
	"github.com/zhanshimian/server/internal/provider"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/account"
	"github.com/zhanshimian/server/internal/service/advisor"
	"github.com/zhanshimian/server/internal/service/billing"
	"github.com/zhanshimian/server/internal/service/diagnostic"
	"github.com/zhanshimian/server/internal/service/execution"
	"github.com/zhanshimian/server/internal/service/feedback"
	"github.com/zhanshimian/server/internal/service/hair"
	"github.com/zhanshimian/server/internal/service/home"
	"github.com/zhanshimian/server/internal/service/media"
	"github.com/zhanshimian/server/internal/service/operation"
	"github.com/zhanshimian/server/internal/service/share"
	"github.com/zhanshimian/server/internal/service/taskrunner"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/service/wardrobe"
	"github.com/zhanshimian/server/internal/storage"
)

type APIApp struct {
	Handler http.Handler
	media   *media.Service
	ops     *operation.Service
	billing *billing.Service
	close   func() error
}

func (a *APIApp) Close() error {
	if a == nil || a.close == nil {
		return nil
	}
	return a.close()
}

type Dependencies struct {
	RunnerFactory func(taskrunner.Options) (*taskrunner.Runner, error)
}

func BuildAPI(cfg config.Config, logger *slog.Logger) (*APIApp, error) {
	return BuildAPIWithDependencies(cfg, logger, Dependencies{})
}

func BuildAPIWithDependencies(cfg config.Config, logger *slog.Logger, deps Dependencies) (*APIApp, error) {
	// API must never construct a worker Runner. RunnerFactory exists so tests
	// can prove this by injecting a factory that fatals if called.
	_ = deps.RunnerFactory

	if err := config.ValidateAPI(cfg); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	ctx := context.Background()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	objects, err := storage.New(storage.Config{
		Provider: cfg.StorageProvider, LocalRoot: cfg.UploadDir,
		COS: storage.COSConfig{BucketURL: cfg.COSBucketURL, SecretID: cfg.COSSecretID, SecretKey: cfg.COSSecretKey, KeyPrefix: cfg.COSKeyPrefix},
	})
	if err != nil {
		pool.Close()
		return nil, err
	}
	store := postgres.New(pool)
	ai, err := BuildAI(cfg, store, objects, logger)
	if err != nil {
		pool.Close()
		return nil, err
	}
	weather, err := buildWeather(cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	wechat, err := buildWeChat(cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	var wechatSession provider.WeChatSessionExchanger
	if exchanger, ok := wechat.(*provider.WeChatCodeExchanger); ok {
		wechatSession = exchanger
	}
	virtualPay, err := buildVirtualPay(cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	wechatApp := buildWeChatApp(cfg, logger)
	apple := buildApple(cfg, logger)
	sms, err := buildSms(cfg, logger)
	if err != nil {
		pool.Close()
		return nil, err
	}

	objectStore, ok := objects.(media.ObjectStore)
	if !ok {
		pool.Close()
		return nil, fmt.Errorf("storage does not support upload grants")
	}
	mediaSvc := media.New(store, objectStore, cfg.MaxUploadBytes, cfg.AssetURLTTL)
	operationSvc := operation.New(store)
	billingSvc := billing.New(store)

	core, err := wireQualityCore(cfg, store, objects, ai)
	if err != nil {
		pool.Close()
		return nil, err
	}
	assessmentSvc := core.Assessment.WithBilling(billingSvc)
	executionSvc := execution.New(store)
	feedbackSvc := feedback.New(store)
	todaySvc := today.New(store, store, providerai.NewTodayPlanner(structuredRuntimeAdapter{ai.Runtime}), todayWeatherAdapter{inner: weather}, today.NewClock())
	wardrobeSvc := wardrobe.New(store, store)
	advisorSvc := advisor.New(store, store, providerai.NewAdvisorChat(structuredRuntimeAdapter{ai.Runtime}))
	diagnosticSvc := diagnostic.New(store, store, providerai.NewDiagnostic(structuredRuntimeAdapter{ai.Runtime}))
	hairStarter := hairStarterAdapter{store: store}
	hairSvc := hair.New(store, store, hairStarter, store)
	var shareSigner share.URLSigner
	if cos, ok := objects.(storage.SignedURLStorage); ok {
		shareSigner = signedURLSigner{inner: cos}
	}
	shareSvc := share.New(store, store, shareSigner, cfg.AssetURLTTL)

	logger.Info("AI capability routes configured", "source", cfg.AIRoutingSource, "routes", ai.Routes)

	// 账户 + 购买订单：替换旧全能 Service 的登录/会话/账号/计费 HTTP 面。
	ordersSvc := billing.NewOrders(store, virtualPay, billingSessionExchanger{inner: wechatSession}, billing.OrdersConfig{
		SKUs: billingSKUsFromConfig(cfg), PaymentEnabled: virtualPay != nil,
	})
	accountSvc := account.New(store, store, store, store, store, wechat, wechatApp, apple, sms,
		avatarURLResolver{objects: objects, publicBaseURL: cfg.PublicBaseURL, ttl: cfg.AssetURLTTL},
		ordersSvc, account.Config{SessionTTL: cfg.SessionTTL, SmsRatePerPhonePerHour: cfg.SmsRatePerPhonePerHour})

	root := http.NewServeMux()
	root.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(cfg.AssetDir))))
	root.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadDir))))
	root.Handle("/", httpapi.New(httpapi.Dependencies{
		Media:        mediaSvc,
		Operations:   operationSvc,
		Home:         home.New(store, home.NewClock()),
		Idempotency:  store,
		DeleteObject: deleteObjectAdapter{objects: objects}.Delete,
		Events:       eventWriterAdapter{store: store},
		Jobs:         store,
		Demo:         demoMediaAdapter{store: store},

		Account:    accountSvc,
		Billing:    ordersSvc,
		Assessment: assessmentSvc,
		Planning:   core.Planning,
		Renders:    core.Rendering,
		Execution:  executionSvc,
		Feedback:   feedbackSvc,
		Today:      todaySvc,
		Wardrobe:   wardrobeSvc,
		Advisor:    advisorSvc,
		Diagnostic: diagnosticSvc,
		Share:      shareSvc,
		Hair:       hairSvc,
	}, logger, cfg.DevLoginEnabled, httpapi.RuntimeInfo{
		Environment:           cfg.Environment,
		StorageProvider:       cfg.StorageProvider,
		WeatherProvider:       cfg.WeatherProvider,
		WeChatLoginConfigured: wechat != nil,
		WeChatAppConfigured:   wechatApp != nil,
		AppleLoginConfigured:  apple != nil,
		SmsProvider:           cfg.SmsProvider,
		AIRoutes:              ai.Routes,
	}))
	return &APIApp{
		Handler: root,
		media:   mediaSvc,
		ops:     operationSvc,
		billing: billingSvc,
		close: func() error {
			pool.Close()
			return nil
		},
	}, nil
}

func buildWeChat(cfg config.Config) (provider.WeChatAuthenticator, error) {
	if cfg.WeChatAppID == "" || cfg.WeChatAppSecret == "" {
		if cfg.DevLoginEnabled {
			return nil, nil
		}
		return nil, errors.New("wechat credentials are required when development login is disabled")
	}
	return provider.NewWeChatCodeExchanger(provider.WeChatConfig{
		AppID: cfg.WeChatAppID, AppSecret: cfg.WeChatAppSecret,
		BaseURL: cfg.WeChatAPIBaseURL, Timeout: cfg.WeChatRequestTimeout,
	}, nil)
}

func buildWeChatApp(cfg config.Config, logger *slog.Logger) provider.WeChatAuthenticator {
	if cfg.WeChatOpenAppID == "" || cfg.WeChatOpenAppSecret == "" {
		return nil
	}
	exchanger, err := provider.NewWeChatAppExchanger(provider.WeChatConfig{
		AppID: cfg.WeChatOpenAppID, AppSecret: cfg.WeChatOpenAppSecret,
		Timeout: cfg.WeChatRequestTimeout,
	}, nil)
	if err != nil {
		logger.Warn("wechat app login disabled", "error", err)
		return nil
	}
	return exchanger
}

func buildApple(cfg config.Config, logger *slog.Logger) provider.AppleAuthenticator {
	if cfg.AppleBundleID == "" {
		return nil
	}
	verifier, err := provider.NewAppleIDVerifier(cfg.AppleBundleID, nil)
	if err != nil {
		logger.Warn("apple login disabled", "error", err)
		return nil
	}
	return verifier
}

func buildSms(cfg config.Config, logger *slog.Logger) (provider.SmsSender, error) {
	if cfg.SmsProvider == "aliyun" {
		return provider.NewAliyunSms(provider.AliyunSmsConfig{
			AccessKeyID:     cfg.AliyunSmsAccessKeyID,
			AccessKeySecret: cfg.AliyunSmsAccessKeySecret,
			SignName:        cfg.AliyunSmsSign,
			TemplateCode:    cfg.AliyunSmsTemplateCode,
			Timeout:         5 * time.Second,
		}, nil)
	}
	return provider.NewConsoleSms(logger, cfg.Environment == "development"), nil
}

func buildVirtualPay(cfg config.Config) (provider.VirtualPayer, error) {
	if !cfg.BillingPaymentEnabled {
		return nil, nil
	}
	return provider.NewWeChatVirtualPay(provider.VirtualPayConfig{
		AppID: cfg.WeChatAppID, AppSecret: cfg.WeChatAppSecret,
		OfferID: cfg.VirtualPayOfferID, AppKey: cfg.VirtualPayAppKey,
		Env: cfg.VirtualPayEnv, APIBase: cfg.VirtualPayAPIBase,
	}, nil)
}

func billingSKUsFromConfig(cfg config.Config) []domain.BillingSKU {
	items := make([]domain.BillingSKU, 0, len(cfg.BillingSKUs))
	for _, sku := range cfg.BillingSKUs {
		items = append(items, domain.BillingSKU{ID: sku.ID, Title: sku.Title, Credits: sku.Credits, PriceFen: sku.PriceFen, ProductID: sku.ProductID})
	}
	return items
}

func buildWeather(cfg config.Config) (provider.WeatherProvider, error) {
	if cfg.WeatherProvider == "demo" {
		return provider.NewDemoWeatherProvider(), nil
	}
	return provider.NewAMapWeatherProvider(provider.AMapWeatherConfig{
		APIKey: cfg.AMapWebServiceKey, BaseURL: cfg.AMapAPIBaseURL,
		DefaultCity: cfg.AMapDefaultCity, DefaultCode: cfg.AMapDefaultAdcode,
		Timeout: cfg.WeatherRequestTimeout,
	}, nil)
}
