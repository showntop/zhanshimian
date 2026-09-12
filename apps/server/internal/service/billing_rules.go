package service

import (
	"errors"
	"fmt"
)

const (
	limitAnalysisPerDay   = 2
	limitLooksPerDay      = 8
	limitLooksConcurrent  = 2
	limitDiagnosticPerDay = 8
	limitAdvisorPerDay    = 20
	limitAdvisorPerHour   = 10
	limitOrdersPerMinute  = 5
)

type billingState struct {
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

type authorizeInput struct {
	Action      string
	Count       int
	WelcomeKind string
}

type billingDecision struct {
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
	Err                 error
}

func decideAuthorize(state billingState, input authorizeInput) billingDecision {
	if input.Count <= 0 {
		input.Count = 1
	}
	switch input.Action {
	case domainActionAnalysis:
		return decideAnalysis(state, input)
	case domainActionLook:
		return decideLook(state, input)
	case domainActionDiagnostic:
		return decideDiagnostic(state, input)
	case domainActionAdvisor:
		return decideAdvisor(state, input)
	case domainActionOrder:
		return decideOrder(state, input)
	default:
		return billingDecision{Err: fmt.Errorf("%w: 不支持的计费动作", ErrValidation)}
	}
}

const (
	domainActionAnalysis   = "analysis"
	domainActionLook       = "look"
	domainActionDiagnostic = "diagnostic"
	domainActionAdvisor    = "advisor"
	domainActionOrder      = "order"
)

func decideAnalysis(state billingState, input authorizeInput) billingDecision {
	if state.DayAnalysis+input.Count > limitAnalysisPerDay {
		return billingDecision{Err: fmt.Errorf("%w: 今日形象分析次数已用完，明天再来", ErrRateLimited)}
	}
	decision := billingDecision{DayAnalysisDelta: input.Count}
	if input.WelcomeKind == "analysis" && !state.WelcomeAnalysisUsed {
		used := true
		decision.WelcomeAnalysisUsed = &used
		decision.LedgerReason = "welcome"
		return decision
	}
	if state.Credits < input.Count {
		return billingDecision{Err: fmt.Errorf("%w: 额度不足，购买次数后可继续形象分析或形象方案制作", ErrInsufficientCredits)}
	}
	decision.CreditsDelta = -input.Count
	decision.LedgerReason = "reserve"
	return decision
}

func decideLook(state billingState, input authorizeInput) billingDecision {
	if state.DayLooks+input.Count > limitLooksPerDay {
		return billingDecision{Err: fmt.Errorf("%w: 今日形象方案制作次数已用完，明天再来", ErrRateLimited)}
	}
	if input.Count == 1 && state.ActiveLooks >= limitLooksConcurrent {
		return billingDecision{Err: fmt.Errorf("%w: 请等待当前形象方案制作完成后再试", ErrRateLimited)}
	}
	if input.Count > 1 && state.ActiveLooks+input.Count > limitLooksPerDay {
		return billingDecision{Err: fmt.Errorf("%w: 请等待当前形象方案制作完成后再试", ErrRateLimited)}
	}
	decision := billingDecision{DayLooksDelta: input.Count}
	if input.WelcomeKind == "plan_set" && !state.WelcomePlanSetUsed {
		used := true
		decision.WelcomePlanSetUsed = &used
		decision.LedgerReason = "welcome"
		return decision
	}
	if state.Credits < input.Count {
		return billingDecision{Err: fmt.Errorf("%w: 额度不足，购买次数后可继续形象分析或形象方案制作", ErrInsufficientCredits)}
	}
	decision.CreditsDelta = -input.Count
	decision.LedgerReason = "reserve"
	return decision
}

func decideDiagnostic(state billingState, input authorizeInput) billingDecision {
	if state.DayDiagnostics+input.Count > limitDiagnosticPerDay {
		return billingDecision{Err: fmt.Errorf("%w: 今日诊断次数已用完，明天再来", ErrRateLimited)}
	}
	return billingDecision{DayDiagnosticsDelta: input.Count, LedgerReason: "reserve"}
}

func decideAdvisor(state billingState, input authorizeInput) billingDecision {
	if state.DayAdvisor+input.Count > limitAdvisorPerDay {
		return billingDecision{Err: fmt.Errorf("%w: 今日咨询次数已用完，明天再来", ErrRateLimited)}
	}
	if state.HourAdvisor+input.Count > limitAdvisorPerHour {
		return billingDecision{Err: fmt.Errorf("%w: 咨询过于频繁，请稍后再试", ErrRateLimited)}
	}
	return billingDecision{DayAdvisorDelta: input.Count, HourAdvisorDelta: input.Count, LedgerReason: "reserve"}
}

func decideOrder(state billingState, input authorizeInput) billingDecision {
	if state.MinuteOrders+input.Count > limitOrdersPerMinute {
		return billingDecision{Err: fmt.Errorf("%w: 下单过于频繁，请稍后再试", ErrRateLimited)}
	}
	return billingDecision{MinuteOrderDelta: input.Count, LedgerReason: "reserve"}
}

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

var (
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrPaymentUnavailable  = errors.New("payment unavailable")
)
