package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	identitypayment "github.com/zhanshimian/server/internal/provider/payment"
)

// 下单频率限制（与 legacy billing_rules 一致）。
const limitOrdersPerMinute = 5

// 错误哨兵：传输层据此映射 429/400。ErrInsufficientCredits/ErrValidation
// 已在 ports.go 旁的 service.go 声明，这里不再重复。
var (
	ErrPaymentUnavailable = errors.New("payment unavailable")
	ErrRateLimited        = errors.New("rate limited")
	ErrValidation         = errors.New("validation error")
)

// OrdersRepository 是订单/权益快照所需的窄端口（*postgres.Store 直接实现）。
type OrdersRepository interface {
	GetBillingWallet(ctx context.Context, userID string) (domain.BillingWallet, error)
	GetBillingUsage(ctx context.Context, userID string, day, hour, minute time.Time) (analysis, looks, diagnostics, advisor, advisorHour, orders int, err error)
	ApplyBilling(ctx context.Context, userID string, now time.Time, activeLooks int, decide func(domain.BillingSnapshot) (domain.BillingDecision, error)) error
	CreateBillingOrder(ctx context.Context, row domain.BillingOrderRow) (domain.BillingOrderRow, error)
	GetBillingOrder(ctx context.Context, userID, orderID string) (domain.BillingOrderRow, error)
	GetBillingOrderByOutTradeNo(ctx context.Context, outTradeNo string) (domain.BillingOrderRow, error)
	FulfillBillingOrder(ctx context.Context, outTradeNo, wxOrderID string) (domain.BillingOrderRow, bool, error)
}

// PaymentGateway 复用 payment 子包的既适配器端口（不重写签名/查单算法）。
type PaymentGateway interface {
	Enabled() bool
	SignGoodsOrder(ctx context.Context, order identitypayment.VirtualPayOrder) (identitypayment.VirtualPayParams, error)
	QueryOrder(ctx context.Context, outTradeNo string) (bool, string, error)
	ParseDeliverNotify(raw []byte, signature string) (identitypayment.VirtualPayNotify, error)
}

// SessionExchanger 提供下单签名所需的 session_key（可选）。
type SessionExchanger interface {
	ExchangeSession(ctx context.Context, code string) (identitypayment.WeChatSession, error)
}

// OrdersConfig 携带 SKU 目录与支付开关。
type OrdersConfig struct {
	SKUs           []domain.BillingSKU
	PaymentEnabled bool
}

// Orders 服务承接购买订单的 HTTP 面：摘要、下单、同步、回调。
type Orders struct {
	repo    OrdersRepository
	gateway PaymentGateway
	session SessionExchanger
	cfg     OrdersConfig
}

func NewOrders(repo OrdersRepository, gateway PaymentGateway, session SessionExchanger, cfg OrdersConfig) *Orders {
	return &Orders{repo: repo, gateway: gateway, session: session, cfg: cfg}
}

func (o *Orders) paymentEnabled() bool { return o.gateway != nil && o.gateway.Enabled() }

func (o *Orders) skuByID(id string) (domain.BillingSKU, bool) {
	for _, sku := range o.cfg.SKUs {
		if sku.ID == id {
			return sku, true
		}
	}
	return domain.BillingSKU{}, false
}

// BillingSummary 汇总钱包、欢迎额度与当日剩余次数。
func (o *Orders) BillingSummary(ctx context.Context, userID string) (domain.BillingSummary, error) {
	wallet, err := o.repo.GetBillingWallet(ctx, userID)
	if err != nil {
		return domain.BillingSummary{}, err
	}
	now := time.Now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	hour := now.Truncate(time.Hour)
	minute := now.Truncate(time.Minute)
	analysis, looks, diagnostics, advisor, _, orders, err := o.repo.GetBillingUsage(ctx, userID, day, hour, minute)
	if err != nil {
		return domain.BillingSummary{}, err
	}
	state := billingState{
		DayAnalysis: analysis, DayLooks: looks, DayDiagnostics: diagnostics, DayAdvisor: advisor, MinuteOrders: orders,
	}
	a, l, d, v := dailyRemaining(state)
	return domain.BillingSummary{
		Credits:                  wallet.Credits,
		WelcomeAnalysisAvailable: !wallet.WelcomeAnalysisUsed,
		WelcomePlanSetAvailable:  !wallet.WelcomePlanSetUsed,
		DailyRemaining:           domain.BillingDailyRemaining{Analysis: a, Looks: l, Diagnostics: d, Advisor: v},
		PaymentEnabled:           o.paymentEnabled(),
		SKUs:                     o.cfg.SKUs,
	}, nil
}

// CreateBillingOrder 下单：分钟级限流 → 落单 → 签名。
func (o *Orders) CreateBillingOrder(ctx context.Context, userID, skuID, loginCode string) (domain.BillingOrder, error) {
	if !o.paymentEnabled() {
		return domain.BillingOrder{}, ErrPaymentUnavailable
	}
	sku, ok := o.skuByID(skuID)
	if !ok {
		return domain.BillingOrder{}, fmt.Errorf("%w: 无效的次数包", ErrValidation)
	}
	// 下单频率：每分钟 5 单（与 legacy decideOrder 一致）。
	err := o.repo.ApplyBilling(ctx, userID, time.Now().UTC(), 0, func(snap domain.BillingSnapshot) (domain.BillingDecision, error) {
		if snap.MinuteOrders+1 > limitOrdersPerMinute {
			return domain.BillingDecision{}, fmt.Errorf("%w: 下单过于频繁，请稍后再试", ErrRateLimited)
		}
		return domain.BillingDecision{MinuteOrderDelta: 1, LedgerReason: "reserve"}, nil
	})
	if err != nil {
		return domain.BillingOrder{}, err
	}
	sessionKey := ""
	if loginCode != "" && o.session != nil {
		session, err := o.session.ExchangeSession(ctx, loginCode)
		if err != nil {
			return domain.BillingOrder{}, err
		}
		sessionKey = session.SessionKey
	}
	outTradeNo := strings.ReplaceAll(uuid.NewString(), "-", "")
	row, err := o.repo.CreateBillingOrder(ctx, domain.BillingOrderRow{
		UserID: userID, SKUID: sku.ID, ProductID: sku.ProductID, Credits: sku.Credits, AmountFen: sku.PriceFen, OutTradeNo: outTradeNo,
	})
	if err != nil {
		return domain.BillingOrder{}, err
	}
	params, err := o.gateway.SignGoodsOrder(ctx, identitypayment.VirtualPayOrder{
		OutTradeNo: outTradeNo, ProductID: sku.ProductID, PriceFen: sku.PriceFen, SessionKey: sessionKey,
	})
	if err != nil {
		return domain.BillingOrder{}, err
	}
	order := o.orderView(row)
	order.SignData = params.SignData
	order.PaySig = params.PaySig
	order.Signature = params.Signature
	order.Mode = params.Mode
	return order, nil
}

// SyncBillingOrder 主动查单并在已支付时履约。
func (o *Orders) SyncBillingOrder(ctx context.Context, userID, orderID string) (domain.BillingOrder, error) {
	row, err := o.repo.GetBillingOrder(ctx, userID, orderID)
	if err != nil {
		return domain.BillingOrder{}, err
	}
	return o.fulfillIfPaid(ctx, row)
}

// HandleBillingNotify 处理支付回调：验签 → 按 out_trade_no 履约。
func (o *Orders) HandleBillingNotify(ctx context.Context, raw []byte, signature string) error {
	if !o.paymentEnabled() {
		return ErrPaymentUnavailable
	}
	notify, err := o.gateway.ParseDeliverNotify(raw, signature)
	if err != nil {
		return err
	}
	row, err := o.repo.GetBillingOrderByOutTradeNo(ctx, notify.OutTradeNo)
	if err != nil {
		return err
	}
	_, err = o.fulfillIfPaid(ctx, row)
	return err
}

func (o *Orders) fulfillIfPaid(ctx context.Context, row domain.BillingOrderRow) (domain.BillingOrder, error) {
	if row.Status == domain.OrderFulfilled || row.Status == domain.OrderClosed || row.Status == domain.OrderRefunded {
		return o.orderView(row), nil
	}
	if !o.paymentEnabled() {
		return domain.BillingOrder{}, ErrPaymentUnavailable
	}
	paid, wxOrderID, err := o.gateway.QueryOrder(ctx, row.OutTradeNo)
	if err != nil {
		return domain.BillingOrder{}, err
	}
	if !paid {
		return o.orderView(row), nil
	}
	fulfilled, _, err := o.repo.FulfillBillingOrder(ctx, row.OutTradeNo, wxOrderID)
	if err != nil {
		return domain.BillingOrder{}, err
	}
	return o.orderView(fulfilled), nil
}

func (o *Orders) orderView(row domain.BillingOrderRow) domain.BillingOrder {
	sku, _ := o.skuByID(row.SKUID)
	return domain.BillingOrder{
		ID: row.ID, SKU: sku, Credits: row.Credits, AmountFen: row.AmountFen,
		OutTradeNo: row.OutTradeNo, Status: row.Status, CreatedAt: row.CreatedAt,
	}
}

// billingState/remaining 与 legacy billing_rules 同义；这里只保留
// BillingSummary 与订单限流用到的部分（完整决策算法随 quality-core
// 的 Reserver 语义迁移进 service/billing 的 Lifecycle 实现）。
type billingState struct {
	DayAnalysis    int
	DayLooks       int
	DayDiagnostics int
	DayAdvisor     int
	MinuteOrders   int
}

const (
	limitAnalysisPerDay   = 2
	limitLooksPerDay      = 8
	limitDiagnosticPerDay = 8
	limitAdvisorPerDay    = 20
)

func remaining(used, limit int) int {
	if used >= limit {
		return 0
	}
	return limit - used
}

func dailyRemaining(state billingState) (analysis, looks, diagnostics, advisor int) {
	return remaining(state.DayAnalysis, limitAnalysisPerDay),
		remaining(state.DayLooks, limitLooksPerDay),
		remaining(state.DayDiagnostics, limitDiagnosticPerDay),
		remaining(state.DayAdvisor, limitAdvisorPerDay)
}
