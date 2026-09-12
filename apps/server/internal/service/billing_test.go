package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
)

type memoryBillingRepo struct {
	repository.Repository
	mu     sync.Mutex
	wallet domain.BillingWallet
	usage  map[string]int
	ledger []domain.BillingLedgerEntry
	orders map[string]domain.BillingOrderRow
	active int
	applyN int
}

func newMemoryBillingRepo() *memoryBillingRepo {
	return &memoryBillingRepo{
		wallet: domain.BillingWallet{UserID: "user-1", Credits: 1},
		usage:  map[string]int{},
		orders: map[string]domain.BillingOrderRow{},
	}
}

func (r *memoryBillingRepo) CountActiveTasksByTypes(context.Context, string, []string) (int, error) {
	return r.active, nil
}

func (r *memoryBillingRepo) GetBillingWallet(_ context.Context, userID string) (domain.BillingWallet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wallet := r.wallet
	wallet.UserID = userID
	return wallet, nil
}

func (r *memoryBillingRepo) GetBillingUsage(context.Context, string, time.Time, time.Time, time.Time) (int, int, int, int, int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.usage["analysis"], r.usage["look"], r.usage["diagnostic"], r.usage["advisor"], r.usage["advisor_hour"], r.usage["order"], nil
}

func (r *memoryBillingRepo) ApplyBilling(_ context.Context, userID string, _ time.Time, activeLooks int, decide func(domain.BillingSnapshot) (domain.BillingDecision, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applyN++
	decision, err := decide(domain.BillingSnapshot{
		Credits:             r.wallet.Credits,
		WelcomeAnalysisUsed: r.wallet.WelcomeAnalysisUsed,
		WelcomePlanSetUsed:  r.wallet.WelcomePlanSetUsed,
		DayAnalysis:         r.usage["analysis"],
		DayLooks:            r.usage["look"],
		DayDiagnostics:      r.usage["diagnostic"],
		DayAdvisor:          r.usage["advisor"],
		HourAdvisor:         r.usage["advisor_hour"],
		MinuteOrders:        r.usage["order"],
		ActiveLooks:         activeLooks,
	})
	if err != nil {
		return err
	}
	credits := r.wallet.Credits + decision.CreditsDelta
	if credits < 0 {
		return errors.New("billing credits would be negative")
	}
	r.wallet.Credits = credits
	r.wallet.UserID = userID
	if decision.WelcomeAnalysisUsed != nil {
		r.wallet.WelcomeAnalysisUsed = *decision.WelcomeAnalysisUsed
	}
	if decision.WelcomePlanSetUsed != nil {
		r.wallet.WelcomePlanSetUsed = *decision.WelcomePlanSetUsed
	}
	r.usage["analysis"] += decision.DayAnalysisDelta
	r.usage["look"] += decision.DayLooksDelta
	r.usage["diagnostic"] += decision.DayDiagnosticsDelta
	r.usage["advisor"] += decision.DayAdvisorDelta
	r.usage["advisor_hour"] += decision.HourAdvisorDelta
	r.usage["order"] += decision.MinuteOrderDelta
	unit := 0
	if decision.CreditsDelta < 0 && len(decision.Refs) > 0 {
		unit = decision.CreditsDelta / len(decision.Refs)
	}
	for _, ref := range decision.Refs {
		r.ledger = append(r.ledger, domain.BillingLedgerEntry{
			UserID: userID, Delta: unit, Reason: decision.LedgerReason, RefType: domain.BillingRefReservation, RefID: ref,
		})
	}
	return nil
}

func (r *memoryBillingRepo) RefundBilling(_ context.Context, refType, refID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var entry *domain.BillingLedgerEntry
	for i := range r.ledger {
		item := &r.ledger[i]
		if item.RefType == refType && item.RefID == refID && (item.Reason == domain.LedgerReserve || item.Reason == domain.LedgerWelcome) {
			entry = item
			break
		}
	}
	if entry == nil {
		return nil
	}
	for _, item := range r.ledger {
		if item.RefType == refType && item.RefID == refID && item.Reason == domain.LedgerRefund {
			return nil
		}
	}
	creditBack := 0
	if entry.Delta < 0 {
		creditBack = -entry.Delta
	}
	r.wallet.Credits += creditBack
	r.ledger = append(r.ledger, domain.BillingLedgerEntry{
		UserID: entry.UserID, Delta: creditBack, Reason: domain.LedgerRefund, RefType: refType, RefID: refID,
	})
	return nil
}

func (r *memoryBillingRepo) RelinkBillingRefs(_ context.Context, fromRefs, toRefs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, from := range fromRefs {
		for j := range r.ledger {
			if r.ledger[j].RefType == domain.BillingRefReservation && r.ledger[j].RefID == from {
				r.ledger[j].RefType = domain.BillingRefTask
				r.ledger[j].RefID = toRefs[i]
			}
		}
	}
	return nil
}

func (r *memoryBillingRepo) CreateBillingOrder(_ context.Context, row domain.BillingOrderRow) (domain.BillingOrderRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row.ID = "order-1"
	row.Status = domain.OrderCreated
	row.CreatedAt = time.Now()
	r.orders[row.OutTradeNo] = row
	r.orders[row.ID] = row
	return row, nil
}

func (r *memoryBillingRepo) GetBillingOrder(_ context.Context, _, orderID string) (domain.BillingOrderRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.orders[orderID]
	if !ok {
		return domain.BillingOrderRow{}, repository.ErrNotFound
	}
	return row, nil
}

func (r *memoryBillingRepo) GetBillingOrderByOutTradeNo(_ context.Context, outTradeNo string) (domain.BillingOrderRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.orders[outTradeNo]
	if !ok {
		return domain.BillingOrderRow{}, repository.ErrNotFound
	}
	return row, nil
}

func (r *memoryBillingRepo) GetUserProfile(context.Context, string) (domain.UserProfile, error) {
	return domain.UserProfile{}, repository.ErrNotFound
}

func (r *memoryBillingRepo) CreateAnalysis(_ context.Context, _ string, input domain.CreateAnalysisInput) (domain.Analysis, *domain.Task, error) {
	return domain.Analysis{ID: "analysis-1", MediaIDs: input.MediaIDs}, &domain.Task{ID: "task-1", Type: string(domain.TaskTypeAnalysis)}, nil
}

func (r *memoryBillingRepo) GetMediaAssetsForUser(context.Context, string, []string) ([]domain.MediaAsset, error) {
	return nil, nil
}

func (r *memoryBillingRepo) FulfillBillingOrder(_ context.Context, outTradeNo, wxOrderID string) (domain.BillingOrderRow, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.orders[outTradeNo]
	if !ok {
		return domain.BillingOrderRow{}, false, repository.ErrNotFound
	}
	if row.Status == domain.OrderFulfilled {
		return row, false, nil
	}
	if row.Status != domain.OrderCreated && row.Status != domain.OrderPaid {
		return row, false, repository.ErrNotFound
	}
	row.Status = domain.OrderFulfilled
	row.WxOrderID = wxOrderID
	r.wallet.Credits += row.Credits
	r.orders[outTradeNo] = row
	r.orders[row.ID] = row
	r.ledger = append(r.ledger, domain.BillingLedgerEntry{
		UserID: row.UserID, Delta: row.Credits, Reason: domain.LedgerPurchase, RefType: domain.BillingRefOrder, RefID: row.ID,
	})
	return row, true, nil
}

type fakeVirtualPay struct {
	paid       bool
	wxOrderID  string
	queryCalls int
}

func (f *fakeVirtualPay) Enabled() bool { return true }

func (f *fakeVirtualPay) SignGoodsOrder(context.Context, provider.VirtualPayOrder) (provider.VirtualPayParams, error) {
	return provider.VirtualPayParams{SignData: `{"offerId":"1"}`, PaySig: "pay", Signature: "sig", Mode: "short_series_goods"}, nil
}

func (f *fakeVirtualPay) QueryOrder(context.Context, string) (bool, string, error) {
	f.queryCalls++
	return f.paid, f.wxOrderID, nil
}

func (f *fakeVirtualPay) ParseDeliverNotify(raw []byte, signature string) (provider.VirtualPayNotify, error) {
	if signature != "ok" {
		return provider.VirtualPayNotify{}, errors.New("virtual pay notify signature invalid")
	}
	var payload struct {
		OutTradeNo string `json:"OutTradeNo"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.OutTradeNo == "" {
		return provider.VirtualPayNotify{}, errors.New("virtual pay notify invalid")
	}
	return provider.VirtualPayNotify{OutTradeNo: payload.OutTradeNo, WxOrderID: "wx-1"}, nil
}

func TestAuthorizeConcurrentReserveOnlyOneSucceeds(t *testing.T) {
	repo := newMemoryBillingRepo()
	svc := &Service{repo: repo}
	var ok, fail int
	var countMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.authorize(context.Background(), "user-1", domainActionLook, "", 1)
			countMu.Lock()
			defer countMu.Unlock()
			if err == nil {
				ok++
				return
			}
			fail++
		}()
	}
	wg.Wait()
	if ok != 1 || fail != 1 || repo.wallet.Credits != 0 {
		t.Fatalf("concurrent reserve ok=%d fail=%d credits=%d", ok, fail, repo.wallet.Credits)
	}
}

func TestAuthorizeRefundRestoresCreditsButKeepsDailyUsage(t *testing.T) {
	repo := newMemoryBillingRepo()
	svc := &Service{repo: repo}
	refs, err := svc.authorize(context.Background(), "user-1", domainActionLook, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if repo.wallet.Credits != 0 || repo.usage["look"] != 1 {
		t.Fatalf("after reserve credits=%d looks=%d", repo.wallet.Credits, repo.usage["look"])
	}
	svc.refundRefs(context.Background(), refs)
	if repo.wallet.Credits != 1 {
		t.Fatalf("refund should restore credits, got %d", repo.wallet.Credits)
	}
	if repo.usage["look"] != 1 {
		t.Fatalf("daily usage must not roll back on refund, got %d", repo.usage["look"])
	}
	if _, err := svc.authorize(context.Background(), "user-1", domainActionLook, "", 1); err != nil {
		t.Fatalf("credits restored should allow another reserve: %v", err)
	}
}

func TestCreateAnalysisSkipsChargeWhenActiveTaskExists(t *testing.T) {
	repo := newMemoryBillingRepo()
	repo.active = 1
	svc := &Service{repo: repo}
	input := domain.CreateAnalysisInput{
		MediaIDs: []string{
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222",
			"33333333-3333-3333-3333-333333333333",
		},
	}
	if _, _, err := svc.CreateAnalysis(context.Background(), "user-1", input); err != nil {
		t.Fatal(err)
	}
	if repo.applyN != 0 {
		t.Fatalf("reusing an in-flight analysis must not authorize, applyN=%d", repo.applyN)
	}
}

func TestFulfillIgnoresClientSuccessUntilQuerySaysPaid(t *testing.T) {
	repo := newMemoryBillingRepo()
	repo.wallet.Credits = 0
	pay := &fakeVirtualPay{paid: false}
	svc := &Service{repo: repo, virtualPay: pay, billingSKUs: defaultBillingSKUs()}
	row, err := repo.CreateBillingOrder(context.Background(), domain.BillingOrderRow{
		UserID: "user-1", SKUID: "pack_3", ProductID: "pack_3", Credits: 3, AmountFen: 600, OutTradeNo: "trade-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	order, err := svc.SyncBillingOrder(context.Background(), "user-1", row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != domain.OrderCreated || repo.wallet.Credits != 0 || pay.queryCalls != 1 {
		t.Fatalf("unpaid query must not credit: %#v credits=%d calls=%d", order, repo.wallet.Credits, pay.queryCalls)
	}
}

func TestFulfillNotifyIsIdempotentAndClosedDoesNotCredit(t *testing.T) {
	repo := newMemoryBillingRepo()
	repo.wallet.Credits = 0
	pay := &fakeVirtualPay{paid: true, wxOrderID: "wx-9"}
	svc := &Service{repo: repo, virtualPay: pay, billingSKUs: defaultBillingSKUs()}
	row, err := repo.CreateBillingOrder(context.Background(), domain.BillingOrderRow{
		UserID: "user-1", SKUID: "pack_3", ProductID: "pack_3", Credits: 3, AmountFen: 600, OutTradeNo: "trade-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"OutTradeNo": row.OutTradeNo})
	if err := svc.HandleBillingNotify(context.Background(), raw, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleBillingNotify(context.Background(), raw, "ok"); err != nil {
		t.Fatal(err)
	}
	if repo.wallet.Credits != 3 {
		t.Fatalf("duplicate notify credited %d", repo.wallet.Credits)
	}

	closed, err := repo.CreateBillingOrder(context.Background(), domain.BillingOrderRow{
		UserID: "user-1", SKUID: "pack_3", ProductID: "pack_3", Credits: 3, AmountFen: 600, OutTradeNo: "trade-closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	closed.Status = domain.OrderClosed
	repo.orders[closed.OutTradeNo] = closed
	repo.orders[closed.ID] = closed
	view, err := svc.fulfillIfPaid(context.Background(), closed)
	if err != nil || view.Status != domain.OrderClosed || repo.wallet.Credits != 3 {
		t.Fatalf("closed order must not fulfill: %#v credits=%d err=%v", view, repo.wallet.Credits, err)
	}
}

func TestCreateBillingOrderRequiresPaymentProvider(t *testing.T) {
	svc := &Service{repo: newMemoryBillingRepo(), billingSKUs: defaultBillingSKUs()}
	if _, err := svc.CreateBillingOrder(context.Background(), "user-1", "pack_3", ""); !errors.Is(err, ErrPaymentUnavailable) {
		t.Fatalf("want payment unavailable, got %v", err)
	}
}
