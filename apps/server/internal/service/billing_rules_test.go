package service

import (
	"errors"
	"testing"
)

func TestDecideAuthorizeWelcomeAnalysis(t *testing.T) {
	decision := decideAuthorize(billingState{}, authorizeInput{Action: domainActionAnalysis, Count: 1, WelcomeKind: "analysis"})
	if decision.Err != nil || decision.LedgerReason != "welcome" || decision.CreditsDelta != 0 || decision.WelcomeAnalysisUsed == nil || !*decision.WelcomeAnalysisUsed {
		t.Fatalf("welcome analysis: %#v", decision)
	}
}

func TestDecideAuthorizeRepeatAnalysisChargesCredit(t *testing.T) {
	decision := decideAuthorize(billingState{Credits: 2, WelcomeAnalysisUsed: true}, authorizeInput{Action: domainActionAnalysis, Count: 1, WelcomeKind: "analysis"})
	if decision.Err != nil || decision.CreditsDelta != -1 || decision.LedgerReason != "reserve" {
		t.Fatalf("paid analysis: %#v", decision)
	}
}

func TestDecideAuthorizeAnalysisInsufficient(t *testing.T) {
	decision := decideAuthorize(billingState{WelcomeAnalysisUsed: true}, authorizeInput{Action: domainActionAnalysis, Count: 1})
	if !errors.Is(decision.Err, ErrInsufficientCredits) {
		t.Fatalf("want insufficient credits, got %#v", decision)
	}
}

func TestDecideAuthorizeAnalysisDailyCap(t *testing.T) {
	decision := decideAuthorize(billingState{DayAnalysis: 2, Credits: 9}, authorizeInput{Action: domainActionAnalysis, Count: 1, WelcomeKind: "analysis"})
	if !errors.Is(decision.Err, ErrRateLimited) {
		t.Fatalf("want rate limited, got %#v", decision)
	}
}

func TestDecideAuthorizeWelcomePlanSet(t *testing.T) {
	decision := decideAuthorize(billingState{}, authorizeInput{Action: domainActionLook, Count: 3, WelcomeKind: "plan_set"})
	if decision.Err != nil || decision.LedgerReason != "welcome" || decision.DayLooksDelta != 3 || decision.CreditsDelta != 0 {
		t.Fatalf("welcome plan set: %#v", decision)
	}
}

func TestDecideAuthorizeLooksChargeAndConcurrent(t *testing.T) {
	ok := decideAuthorize(billingState{Credits: 5}, authorizeInput{Action: domainActionLook, Count: 1})
	if ok.Err != nil || ok.CreditsDelta != -1 {
		t.Fatalf("single look: %#v", ok)
	}
	blocked := decideAuthorize(billingState{Credits: 5, ActiveLooks: 2}, authorizeInput{Action: domainActionLook, Count: 1})
	if !errors.Is(blocked.Err, ErrRateLimited) {
		t.Fatalf("concurrent look should 429, got %#v", blocked)
	}
}

func TestDecideAuthorizeLooksDailyCap(t *testing.T) {
	decision := decideAuthorize(billingState{Credits: 9, DayLooks: 6}, authorizeInput{Action: domainActionLook, Count: 3, WelcomeKind: "plan_set"})
	if !errors.Is(decision.Err, ErrRateLimited) {
		t.Fatalf("want daily look cap, got %#v", decision)
	}
}

func TestDecideAuthorizeWelcomeNotTransferableToHair(t *testing.T) {
	decision := decideAuthorize(billingState{Credits: 0}, authorizeInput{Action: domainActionLook, Count: 1})
	if !errors.Is(decision.Err, ErrInsufficientCredits) {
		t.Fatalf("hair without welcome/credits must 402, got %#v", decision)
	}
}

func TestDecideAuthorizeSkipsCreditWhenPaymentOff(t *testing.T) {
	look := decideAuthorize(billingState{Credits: 0}, authorizeInput{Action: domainActionLook, Count: 1, SkipCreditCharge: true})
	if look.Err != nil || look.CreditsDelta != 0 || look.DayLooksDelta != 1 {
		t.Fatalf("look without payment: %#v", look)
	}
	analysis := decideAuthorize(billingState{Credits: 0, WelcomeAnalysisUsed: true}, authorizeInput{Action: domainActionAnalysis, Count: 1, SkipCreditCharge: true})
	if analysis.Err != nil || analysis.CreditsDelta != 0 || analysis.DayAnalysisDelta != 1 {
		t.Fatalf("analysis without payment: %#v", analysis)
	}
	capped := decideAuthorize(billingState{Credits: 0, DayLooks: 8}, authorizeInput{Action: domainActionLook, Count: 1, SkipCreditCharge: true})
	if !errors.Is(capped.Err, ErrRateLimited) {
		t.Fatalf("daily cap still applies when payment is off, got %#v", capped)
	}
}

func TestDecideAuthorizeDiagnosticAndAdvisor(t *testing.T) {
	ok := decideAuthorize(billingState{}, authorizeInput{Action: domainActionDiagnostic, Count: 1})
	if ok.Err != nil || ok.DayDiagnosticsDelta != 1 || ok.CreditsDelta != 0 {
		t.Fatalf("diagnostic: %#v", ok)
	}
	capped := decideAuthorize(billingState{DayDiagnostics: 8}, authorizeInput{Action: domainActionDiagnostic, Count: 1})
	if !errors.Is(capped.Err, ErrRateLimited) {
		t.Fatalf("diagnostic cap: %#v", capped)
	}
	hour := decideAuthorize(billingState{HourAdvisor: 10}, authorizeInput{Action: domainActionAdvisor, Count: 1})
	if !errors.Is(hour.Err, ErrRateLimited) {
		t.Fatalf("advisor hour cap: %#v", hour)
	}
}

func TestDecideAuthorizeOrderRateLimit(t *testing.T) {
	ok := decideAuthorize(billingState{}, authorizeInput{Action: domainActionOrder, Count: 1})
	if ok.Err != nil || ok.MinuteOrderDelta != 1 {
		t.Fatalf("order: %#v", ok)
	}
	capped := decideAuthorize(billingState{MinuteOrders: 5}, authorizeInput{Action: domainActionOrder, Count: 1})
	if !errors.Is(capped.Err, ErrRateLimited) {
		t.Fatalf("order cap: %#v", capped)
	}
}

func TestDailyRemaining(t *testing.T) {
	a, l, d, v := dailyRemaining(billingState{DayAnalysis: 1, DayLooks: 8, DayDiagnostics: 3, DayAdvisor: 0})
	if a != 1 || l != 0 || d != 5 || v != 20 {
		t.Fatalf("remaining analysis=%d looks=%d diagnostics=%d advisor=%d", a, l, d, v)
	}
}
