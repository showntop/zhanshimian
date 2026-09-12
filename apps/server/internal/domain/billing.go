package domain

import (
	"errors"
	"time"
)

const (
	LedgerReserve  = "reserve"
	LedgerSettle   = "settle"
	LedgerRefund   = "refund"
	LedgerWelcome  = "welcome"
	LedgerPurchase = "purchase"

	BillingRefOperation   = "operation"
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

type BillingReservationStatus string

const (
	BillingReserved BillingReservationStatus = "reserved"
	BillingSettled  BillingReservationStatus = "settled"
	BillingRefunded BillingReservationStatus = "refunded"
)

// Product 是计费绑定到的业务产物,与 billing_reservations.product 对应。
type Product string

const (
	ProductAssessment        Product = "assessment"
	ProductPlanSet           Product = "plan_set"
	ProductRenderPublication Product = "render_publication"
)

// ChargeSource 是费用来源;welcome_* 走免费权益,reserve/settle/refund 的 delta 为 0。
type ChargeSource string

const (
	ChargeCredits         ChargeSource = "credits"
	ChargeWelcomeAnalysis ChargeSource = "welcome_analysis"
	ChargeWelcomePlanSet  ChargeSource = "welcome_plan_set"
)

var (
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrAlreadySettled      = errors.New("already settled")
	ErrAlreadyRefunded     = errors.New("already refunded")
	ErrReservationConflict = errors.New("reservation conflict")
)

// Reservation is one immutable billing reservation bound to an operation. It
// mirrors billing_reservations: the product names the business artifact whose
// publication settles the reservation, and charge_source records whether the
// units were drawn from credits or a welcome entitlement.
type Reservation struct {
	ID            string
	UserID        string
	OperationID   string
	Product       Product
	Units         int
	ChargeSource  ChargeSource
	Status        BillingReservationStatus
	PublicationID *string
	CreatedAt     time.Time
	SettledAt     *time.Time
	RefundedAt    *time.Time
}

// ReconcileFailure records one reservation that could not be settled or
// refunded during reconciliation.
type ReconcileFailure struct {
	OperationID string
	Err         error
}

// ReconcileResult summarizes one reconciliation sweep over terminal operations.
type ReconcileResult struct {
	Settled  int
	Refunded int
	Failed   []ReconcileFailure
}

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
	ID               string `json:"id"`
	Title            string `json:"title"`
	Credits          int    `json:"credits"`
	PriceFen         int    `json:"price_fen"`
	OriginalPriceFen int    `json:"original_price_fen,omitempty"`
	Badge            string `json:"badge,omitempty"`
	ProductID        string `json:"product_id"`
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
