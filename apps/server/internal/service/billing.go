package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
)

func (s *Service) authorize(ctx context.Context, userID, action, welcomeKind string, count int) ([]string, error) {
	if count <= 0 {
		count = 1
	}
	refs := make([]string, count)
	for i := range refs {
		refs[i] = uuid.NewString()
	}
	activeLooks := 0
	if action == domainActionLook {
		var err error
		activeLooks, err = s.repo.CountActiveTasksByTypes(ctx, userID, []string{
			string(domain.TaskTypeHairPreview), string(domain.TaskTypePlanLook), string(domain.TaskTypeTodayLook),
		})
		if err != nil {
			return nil, err
		}
	}
	err := s.repo.ApplyBilling(ctx, userID, time.Now(), activeLooks, func(snap domain.BillingSnapshot) (domain.BillingDecision, error) {
		decision := decideAuthorize(billingState{
			Credits:             snap.Credits,
			WelcomeAnalysisUsed: snap.WelcomeAnalysisUsed,
			WelcomePlanSetUsed:  snap.WelcomePlanSetUsed,
			DayAnalysis:         snap.DayAnalysis,
			DayLooks:            snap.DayLooks,
			DayDiagnostics:      snap.DayDiagnostics,
			DayAdvisor:          snap.DayAdvisor,
			HourAdvisor:         snap.HourAdvisor,
			MinuteOrders:        snap.MinuteOrders,
			ActiveLooks:         snap.ActiveLooks,
		}, authorizeInput{Action: action, Count: count, WelcomeKind: welcomeKind})
		if decision.Err != nil {
			return domain.BillingDecision{}, decision.Err
		}
		return domain.BillingDecision{
			CreditsDelta:        decision.CreditsDelta,
			WelcomeAnalysisUsed: decision.WelcomeAnalysisUsed,
			WelcomePlanSetUsed:  decision.WelcomePlanSetUsed,
			DayAnalysisDelta:    decision.DayAnalysisDelta,
			DayLooksDelta:       decision.DayLooksDelta,
			DayDiagnosticsDelta: decision.DayDiagnosticsDelta,
			DayAdvisorDelta:     decision.DayAdvisorDelta,
			HourAdvisorDelta:    decision.HourAdvisorDelta,
			MinuteOrderDelta:    decision.MinuteOrderDelta,
			LedgerReason:        decision.LedgerReason,
			Action:              action,
			Refs:                refs,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

func (s *Service) refundRefs(ctx context.Context, refs []string) {
	for _, ref := range refs {
		if err := s.repo.RefundBilling(ctx, domain.BillingRefReservation, ref); err != nil {
			s.loggerOrDefault().Error("refund billing reservation", "ref", ref, "error", err)
		}
	}
}

func (s *Service) refundTaskCharge(ctx context.Context, taskID string) {
	if err := s.repo.RefundBilling(ctx, domain.BillingRefTask, taskID); err != nil {
		s.loggerOrDefault().Error("refund billing task", "task_id", taskID, "error", err)
	}
}

func (s *Service) bindCharges(ctx context.Context, refs []string, taskIDs []string) {
	if len(refs) == 0 || len(taskIDs) == 0 {
		return
	}
	n := len(refs)
	if len(taskIDs) < n {
		n = len(taskIDs)
	}
	if err := s.repo.RelinkBillingRefs(ctx, refs[:n], taskIDs[:n]); err != nil {
		s.loggerOrDefault().Error("relink billing refs", "error", err)
	}
	if n < len(refs) {
		s.refundRefs(ctx, refs[n:])
	}
}

func (s *Service) sweepFailedTaskCharges(ctx context.Context) {
	items, err := s.repo.ListUnrefundedFailedTaskCharges(ctx)
	if err != nil {
		s.loggerOrDefault().Error("list failed billing charges", "error", err)
		return
	}
	for _, item := range items {
		if err := s.repo.RefundBilling(ctx, item.RefType, item.RefID); err != nil {
			s.loggerOrDefault().Error("sweep refund billing", "ref", item.RefID, "error", err)
		}
	}
}

func (s *Service) BillingSummary(ctx context.Context, userID string) (domain.BillingSummary, error) {
	wallet, err := s.repo.GetBillingWallet(ctx, userID)
	if err != nil {
		return domain.BillingSummary{}, err
	}
	now := time.Now()
	day, hour, minute := now.UTC(), now.UTC().Truncate(time.Hour), now.UTC().Truncate(time.Minute)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	analysis, looks, diagnostics, advisor, _, _, err := s.repo.GetBillingUsage(ctx, userID, day, hour, minute)
	if err != nil {
		return domain.BillingSummary{}, err
	}
	a, l, d, v := dailyRemaining(billingState{DayAnalysis: analysis, DayLooks: looks, DayDiagnostics: diagnostics, DayAdvisor: advisor})
	return domain.BillingSummary{
		Credits:                  wallet.Credits,
		WelcomeAnalysisAvailable: !wallet.WelcomeAnalysisUsed,
		WelcomePlanSetAvailable:  !wallet.WelcomePlanSetUsed,
		DailyRemaining:           domain.BillingDailyRemaining{Analysis: a, Looks: l, Diagnostics: d, Advisor: v},
		PaymentEnabled:           s.paymentEnabled(),
		SKUs:                     s.billingSKUs,
	}, nil
}

func (s *Service) paymentEnabled() bool {
	return s.virtualPay != nil && s.virtualPay.Enabled()
}

func (s *Service) CreateBillingOrder(ctx context.Context, userID, skuID, loginCode string) (domain.BillingOrder, error) {
	if !s.paymentEnabled() {
		return domain.BillingOrder{}, ErrPaymentUnavailable
	}
	sku, ok := s.skuByID(skuID)
	if !ok {
		return domain.BillingOrder{}, fmt.Errorf("%w: 无效的次数包", ErrValidation)
	}
	if _, err := s.authorize(ctx, userID, domainActionOrder, "", 1); err != nil {
		return domain.BillingOrder{}, err
	}
	sessionKey := ""
	if loginCode != "" && s.wechatSession != nil {
		session, err := s.wechatSession.ExchangeSession(ctx, loginCode)
		if err != nil {
			return domain.BillingOrder{}, err
		}
		sessionKey = session.SessionKey
	}
	outTradeNo := strings.ReplaceAll(uuid.NewString(), "-", "")
	row, err := s.repo.CreateBillingOrder(ctx, domain.BillingOrderRow{
		UserID: userID, SKUID: sku.ID, ProductID: sku.ProductID, Credits: sku.Credits, AmountFen: sku.PriceFen, OutTradeNo: outTradeNo,
	})
	if err != nil {
		return domain.BillingOrder{}, err
	}
	params, err := s.virtualPay.SignGoodsOrder(ctx, provider.VirtualPayOrder{
		OutTradeNo: outTradeNo, ProductID: sku.ProductID, PriceFen: sku.PriceFen, SessionKey: sessionKey,
	})
	if err != nil {
		return domain.BillingOrder{}, err
	}
	return domain.BillingOrder{
		ID: row.ID, SKU: sku, Credits: row.Credits, AmountFen: row.AmountFen, OutTradeNo: row.OutTradeNo,
		Status: row.Status, SignData: params.SignData, PaySig: params.PaySig, Signature: params.Signature,
		Mode: params.Mode, CreatedAt: row.CreatedAt,
	}, nil
}

func (s *Service) SyncBillingOrder(ctx context.Context, userID, orderID string) (domain.BillingOrder, error) {
	row, err := s.repo.GetBillingOrder(ctx, userID, orderID)
	if err != nil {
		return domain.BillingOrder{}, err
	}
	return s.fulfillIfPaid(ctx, row)
}

func (s *Service) HandleBillingNotify(ctx context.Context, raw []byte, signature string) error {
	if !s.paymentEnabled() {
		return ErrPaymentUnavailable
	}
	notify, err := s.virtualPay.ParseDeliverNotify(raw, signature)
	if err != nil {
		return err
	}
	row, err := s.repo.GetBillingOrderByOutTradeNo(ctx, notify.OutTradeNo)
	if err != nil {
		return err
	}
	_, err = s.fulfillIfPaid(ctx, row)
	return err
}

func (s *Service) fulfillIfPaid(ctx context.Context, row domain.BillingOrderRow) (domain.BillingOrder, error) {
	if row.Status == domain.OrderFulfilled || row.Status == domain.OrderClosed || row.Status == domain.OrderRefunded {
		return s.orderView(row), nil
	}
	if !s.paymentEnabled() {
		return domain.BillingOrder{}, ErrPaymentUnavailable
	}
	paid, wxOrderID, err := s.virtualPay.QueryOrder(ctx, row.OutTradeNo)
	if err != nil {
		return domain.BillingOrder{}, err
	}
	if !paid {
		return s.orderView(row), nil
	}
	fulfilled, _, err := s.repo.FulfillBillingOrder(ctx, row.OutTradeNo, wxOrderID)
	if err != nil {
		return domain.BillingOrder{}, err
	}
	return s.orderView(fulfilled), nil
}

func (s *Service) orderView(row domain.BillingOrderRow) domain.BillingOrder {
	sku, _ := s.skuByID(row.SKUID)
	return domain.BillingOrder{
		ID: row.ID, SKU: sku, Credits: row.Credits, AmountFen: row.AmountFen,
		OutTradeNo: row.OutTradeNo, Status: row.Status, CreatedAt: row.CreatedAt,
	}
}

func (s *Service) skuByID(id string) (domain.BillingSKU, bool) {
	for _, sku := range s.billingSKUs {
		if sku.ID == id {
			return sku, true
		}
	}
	return domain.BillingSKU{}, false
}

func defaultBillingSKUs() []domain.BillingSKU {
	return []domain.BillingSKU{
		{ID: "pack_3", Title: "体验次数 ×3", Credits: 3, PriceFen: 690, OriginalPriceFen: 990, ProductID: "pack_3"},
		{ID: "pack_10", Title: "常用次数 ×10", Credits: 10, PriceFen: 1690, OriginalPriceFen: 2990, Badge: "featured", ProductID: "pack_10"},
		{ID: "pack_30", Title: "超值次数 ×30", Credits: 30, PriceFen: 4990, OriginalPriceFen: 9990, Badge: "value", ProductID: "pack_30"},
	}
}
