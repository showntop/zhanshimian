package domain

import "time"

const (
	LedgerReserve  = "reserve"
	LedgerRefund   = "refund"
	LedgerWelcome  = "welcome"
	LedgerPurchase = "purchase"

	BillingRefReservation = "reservation"
	BillingRefTask        = "task"
	BillingRefOrder       = "order"
	BillingRefUsage       = "usage"

	OrderCreated   = "created"
	OrderPaid      = "paid"
	OrderFulfilled = "fulfilled"
	OrderRefunded  = "refunded"
	OrderClosed    = "closed"

	BillingActionAnalysis   = "analysis"
	BillingActionLook       = "look"
	BillingActionDiagnostic = "diagnostic"
	BillingActionAdvisor    = "advisor"
	BillingActionOrder      = "order"

	WelcomeAnalysis = "analysis"
	WelcomePlanSet  = "plan_set"
)

type BillingWallet struct {
	UserID              string
	Credits             int
	WelcomeAnalysisUsed bool
	WelcomePlanSetUsed  bool
	UpdatedAt           time.Time
}

type BillingLedgerEntry struct {
	ID        string
	UserID    string
	Delta     int
	Reason    string
	RefType   string
	RefID     string
	CreatedAt time.Time
}

type BillingSKU struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Credits   int    `json:"credits"`
	PriceFen  int    `json:"price_fen"`
	ProductID string `json:"product_id"`
}

type BillingDailyRemaining struct {
	Analysis    int `json:"analysis"`
	Looks       int `json:"looks"`
	Diagnostics int `json:"diagnostics"`
	Advisor     int `json:"advisor"`
}

type BillingSummary struct {
	Credits                  int                   `json:"credits"`
	WelcomeAnalysisAvailable bool                  `json:"welcome_analysis_available"`
	WelcomePlanSetAvailable  bool                  `json:"welcome_plan_set_available"`
	DailyRemaining           BillingDailyRemaining `json:"daily_remaining"`
	PaymentEnabled           bool                  `json:"payment_enabled"`
	SKUs                     []BillingSKU          `json:"skus"`
}

type BillingOrder struct {
	ID         string     `json:"id"`
	SKU        BillingSKU `json:"sku"`
	Credits    int        `json:"credits"`
	AmountFen  int        `json:"amount_fen"`
	OutTradeNo string     `json:"out_trade_no"`
	Status     string     `json:"status"`
	SignData   string     `json:"sign_data,omitempty"`
	PaySig     string     `json:"pay_sig,omitempty"`
	Signature  string     `json:"signature,omitempty"`
	Mode       string     `json:"mode,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type BillingSnapshot struct {
	Credits             int
	WelcomeAnalysisUsed bool
	WelcomePlanSetUsed  bool
	DayAnalysis         int
	DayLooks            int
	DayDiagnostics      int
	DayAdvisor          int
	HourAdvisor         int
	MinuteOrders        int
	ActiveLooks         int
}

type BillingDecision struct {
	CreditsDelta        int
	WelcomeAnalysisUsed *bool
	WelcomePlanSetUsed  *bool
	DayAnalysisDelta    int
	DayLooksDelta       int
	DayDiagnosticsDelta int
	DayAdvisorDelta     int
	HourAdvisorDelta    int
	MinuteOrderDelta    int
	LedgerReason        string
	Action              string
	Refs                []string
}

type BillingOrderRow struct {
	ID         string
	UserID     string
	SKUID      string
	ProductID  string
	Credits    int
	AmountFen  int
	OutTradeNo string
	WxOrderID  string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
