package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhanshimian/server/internal/bootstrap"
	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/httpapi"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service"
	"github.com/zhanshimian/server/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		logger.Error("migrate database", "error", err)
		os.Exit(1)
	}
	objects, err := storage.New(storage.Config{
		Provider: cfg.StorageProvider, LocalRoot: cfg.UploadDir,
		COS: storage.COSConfig{BucketURL: cfg.COSBucketURL, SecretID: cfg.COSSecretID, SecretKey: cfg.COSSecretKey, KeyPrefix: cfg.COSKeyPrefix},
	})
	if err != nil {
		logger.Error("create storage", "error", err)
		os.Exit(1)
	}
	repo := postgres.New(pool)
	ai, err := bootstrap.BuildAI(cfg, repo, objects, logger)
	if err != nil {
		logger.Error("create AI capability providers", "routing_source", cfg.AIRoutingSource, "error", err)
		os.Exit(1)
	}
	weather, err := buildWeather(cfg)
	if err != nil {
		logger.Error("create weather provider", "provider", cfg.WeatherProvider, "error", err)
		os.Exit(1)
	}
	wechat, err := buildWeChat(cfg)
	if err != nil {
		logger.Error("create wechat login provider", "error", err)
		os.Exit(1)
	}
	var wechatSession provider.WeChatSessionExchanger
	if exchanger, ok := wechat.(*provider.WeChatCodeExchanger); ok {
		wechatSession = exchanger
	}
	virtualPay, err := buildVirtualPay(cfg)
	if err != nil {
		logger.Error("create virtual pay provider", "error", err)
		os.Exit(1)
	}
	wechatApp := buildWeChatApp(cfg, logger)
	apple := buildApple(cfg, logger)
	sms, err := buildSms(cfg, logger)
	if err != nil {
		logger.Error("create sms provider", "provider", cfg.SmsProvider, "error", err)
		os.Exit(1)
	}
	logger.Info("AI capability routes configured", "source", cfg.AIRoutingSource, "routes", ai.Routes)
	svc := service.New(repo, objects, ai.Analyzer, cfg.PublicBaseURL, cfg.SessionTTL, cfg.MaxUploadBytes, logger, service.ProviderOptions{
		Hair: ai.Hair, Look: ai.Look, PlanGroup: ai.PlanGroup, Outfit: ai.Outfit, Purchase: ai.Purchase, Advisor: ai.Advisor, Today: ai.Today,
		Weather: weather, WeChat: wechat, WeChatApp: wechatApp, Apple: apple, Sms: sms,
		SmsPerPhone: cfg.SmsRatePerPhonePerHour, AssetURLTTL: cfg.AssetURLTTL,
		BillingSKUs: billingSKUsFromConfig(cfg), VirtualPay: virtualPay, WeChatSession: wechatSession,
		AssetDir: cfg.AssetDir,
	})
	if cfg.RunWorker {
		go svc.RunWorker(ctx, cfg.AnalysisPollTime)
	}

	root := http.NewServeMux()
	root.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(cfg.AssetDir))))
	root.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadDir))))
	root.Handle("/", httpapi.New(svc, logger, cfg.DevLoginEnabled, httpapi.RuntimeInfo{
		Environment:           cfg.Environment,
		StorageProvider:       cfg.StorageProvider,
		WeatherProvider:       cfg.WeatherProvider,
		WeChatLoginConfigured: wechat != nil,
		WeChatAppConfigured:   wechatApp != nil,
		AppleLoginConfigured:  apple != nil,
		SmsProvider:           cfg.SmsProvider,
		AIRoutes:              ai.Routes,
	}))
	// 写超时随路由表里最慢的模型走（+10s 余量），保证长生成的响应不被掐断
	writeTimeout := 30 * time.Second
	for _, model := range cfg.AIRouting.Models {
		modelTimeout := time.Duration(model.TimeoutSeconds) * time.Second
		if modelTimeout <= 0 {
			modelTimeout = 90 * time.Second
		}
		if modelTimeout+10*time.Second > writeTimeout {
			writeTimeout = modelTimeout + 10*time.Second
		}
	}
	server := &http.Server{Addr: cfg.Addr, Handler: root, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: writeTimeout, IdleTimeout: 90 * time.Second}
	go func() {
		logger.Info("api started", "addr", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		logger.Error("shutdown", "error", err)
	}
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

// buildWeChatApp：微信开放平台 App OAuth（Android 端）。凭据可选——未配置时
// 该登录方式关闭（生产门禁不强制，因为小程序生产不依赖它）。
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

// buildApple：Apple 登录（iOS 端）。未配置 BundleID 时关闭。
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

// buildSms：短信验证码发送。console 仅供开发（固定码 888888 回传 dev_code），
// 生产配置门禁强制 aliyun。
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
		items = append(items, domain.BillingSKU{
			ID: sku.ID, Title: sku.Title, Credits: sku.Credits, PriceFen: sku.PriceFen,
			OriginalPriceFen: sku.OriginalPriceFen, Badge: sku.Badge, ProductID: sku.ProductID,
		})
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
