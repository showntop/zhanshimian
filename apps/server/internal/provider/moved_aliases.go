// Package provider 的兼容别名：Task 9 将本包的登录/短信/Apple/天气/支付
// 适配器机械迁移到 identity/weather/payment 子包后，legacy service 仍按
// 旧名字引用它们。这里的类型别名让迁移期的旧代码保持可编译；
// legacy service 删除时本文件一并删除，不新增使用方。
package provider

import (
	"github.com/zhanshimian/server/internal/provider/identity"
	"github.com/zhanshimian/server/internal/provider/payment"
	"github.com/zhanshimian/server/internal/provider/weather"
)

type (
	WeatherProvider        = weather.WeatherProvider
	Weather                = weather.Weather
	DemoWeatherProvider    = weather.DemoWeatherProvider
	AMapWeatherProvider    = weather.AMapWeatherProvider
	AMapWeatherConfig      = weather.AMapWeatherConfig
	WeChatAuthenticator    = identity.WeChatAuthenticator
	WeChatIdentity         = identity.WeChatIdentity
	WeChatConfig           = identity.WeChatConfig
	WeChatCodeExchanger    = identity.WeChatCodeExchanger
	WeChatAppExchanger     = identity.WeChatAppExchanger
	AppleAuthenticator     = identity.AppleAuthenticator
	SmsSender              = identity.SmsSender
	AliyunSmsConfig       = identity.AliyunSmsConfig
	VirtualPayer           = payment.VirtualPayer
	VirtualPayConfig       = payment.VirtualPayConfig
	WeChatVirtualPay       = payment.WeChatVirtualPay
	WeChatSessionExchanger = identity.WeChatSessionExchanger
	WeChatSession          = payment.WeChatSession
	VirtualPayOrder        = payment.VirtualPayOrder
	VirtualPayParams       = payment.VirtualPayParams
	VirtualPayNotify       = payment.VirtualPayNotify
)

var (
	NewDemoWeatherProvider = weather.NewDemoWeatherProvider
	NewAMapWeatherProvider = weather.NewAMapWeatherProvider
	NewWeChatCodeExchanger = identity.NewWeChatCodeExchanger
	NewWeChatAppExchanger  = identity.NewWeChatAppExchanger
	NewAppleIDVerifier     = identity.NewAppleIDVerifier
	NewAliyunSms           = identity.NewAliyunSms
	NewConsoleSms          = identity.NewConsoleSms
	NewWeChatVirtualPay    = payment.NewWeChatVirtualPay

	ErrWeChatCodeRejected = identity.ErrWeChatCodeRejected
	ErrWeChatRateLimited  = identity.ErrWeChatRateLimited
	ErrWeChatUnavailable  = identity.ErrWeChatUnavailable
	ErrAppleTokenRejected = identity.ErrAppleTokenRejected
	ErrAppleUnavailable   = identity.ErrAppleUnavailable
	ErrSmsRejected        = identity.ErrSmsRejected
	ErrSmsUnavailable     = identity.ErrSmsUnavailable
	ErrSmsConfig          = identity.ErrSmsConfig
	ErrWeatherUnavailable = weather.ErrWeatherUnavailable
	DevSmsCode            = identity.DevSmsCode
)
