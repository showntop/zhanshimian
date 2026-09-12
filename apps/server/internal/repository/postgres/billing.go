package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func (s *Store) CountActiveTasksByTypes(ctx context.Context, userID string, types []string) (int, error) {
	if len(types) == 0 {
		return 0, nil
	}
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE user_id=$1 AND type=ANY($2) AND status IN ('queued','processing')`, userID, types).Scan(&count)
	return count, err
}

func (s *Store) GetBillingWallet(ctx context.Context, userID string) (domain.BillingWallet, error) {
	var wallet domain.BillingWallet
	err := s.pool.QueryRow(ctx, `
		INSERT INTO billing_wallets(user_id) VALUES($1)
		ON CONFLICT (user_id) DO UPDATE SET user_id=EXCLUDED.user_id
		RETURNING user_id::text,credits,welcome_analysis_used,welcome_plan_set_used,updated_at`, userID).
		Scan(&wallet.UserID, &wallet.Credits, &wallet.WelcomeAnalysisUsed, &wallet.WelcomePlanSetUsed, &wallet.UpdatedAt)
	return wallet, err
}

func (s *Store) GetBillingUsage(ctx context.Context, userID string, day, hour, minute time.Time) (analysis, looks, diagnostics, advisor, advisorHour, orders int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='analysis'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='look'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='diagnostic'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='advisor'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$3 AND action='advisor_hour'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$4 AND action='order'),0)`,
		userID, day, hour, minute).
		Scan(&analysis, &looks, &diagnostics, &advisor, &advisorHour, &orders)
	return
}

func (s *Store) ApplyBilling(ctx context.Context, userID string, now time.Time, activeLooks int, decide func(domain.BillingSnapshot) (domain.BillingDecision, error)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO billing_wallets(user_id) VALUES($1) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return err
	}
	var snap domain.BillingSnapshot
	if err = tx.QueryRow(ctx, `SELECT credits,welcome_analysis_used,welcome_plan_set_used FROM billing_wallets WHERE user_id=$1 FOR UPDATE`, userID).
		Scan(&snap.Credits, &snap.WelcomeAnalysisUsed, &snap.WelcomePlanSetUsed); err != nil {
		return err
	}
	day, hour, minute := usageBuckets(now)
	if err = tx.QueryRow(ctx, `
		SELECT
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='analysis'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='look'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='diagnostic'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$2 AND action='advisor'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$3 AND action='advisor_hour'),0),
			coalesce((SELECT count FROM billing_usage WHERE user_id=$1 AND bucket=$4 AND action='order'),0)`,
		userID, day, hour, minute).
		Scan(&snap.DayAnalysis, &snap.DayLooks, &snap.DayDiagnostics, &snap.DayAdvisor, &snap.HourAdvisor, &snap.MinuteOrders); err != nil {
		return err
	}
	snap.ActiveLooks = activeLooks
	decision, err := decide(snap)
	if err != nil {
		return err
	}
	credits := snap.Credits + decision.CreditsDelta
	if credits < 0 {
		return errors.New("billing credits would be negative")
	}
	welcomeAnalysis := snap.WelcomeAnalysisUsed
	if decision.WelcomeAnalysisUsed != nil {
		welcomeAnalysis = *decision.WelcomeAnalysisUsed
	}
	welcomePlan := snap.WelcomePlanSetUsed
	if decision.WelcomePlanSetUsed != nil {
		welcomePlan = *decision.WelcomePlanSetUsed
	}
	if _, err = tx.Exec(ctx, `UPDATE billing_wallets SET credits=$2,welcome_analysis_used=$3,welcome_plan_set_used=$4,updated_at=now() WHERE user_id=$1`,
		userID, credits, welcomeAnalysis, welcomePlan); err != nil {
		return err
	}
	if err = bumpUsage(ctx, tx, userID, day, "analysis", decision.DayAnalysisDelta); err != nil {
		return err
	}
	if err = bumpUsage(ctx, tx, userID, day, "look", decision.DayLooksDelta); err != nil {
		return err
	}
	if err = bumpUsage(ctx, tx, userID, day, "diagnostic", decision.DayDiagnosticsDelta); err != nil {
		return err
	}
	if err = bumpUsage(ctx, tx, userID, day, "advisor", decision.DayAdvisorDelta); err != nil {
		return err
	}
	if err = bumpUsage(ctx, tx, userID, hour, "advisor_hour", decision.HourAdvisorDelta); err != nil {
		return err
	}
	if err = bumpUsage(ctx, tx, userID, minute, "order", decision.MinuteOrderDelta); err != nil {
		return err
	}
	if decision.LedgerReason != "" && len(decision.Refs) > 0 {
		unit := 0
		if decision.CreditsDelta < 0 {
			unit = decision.CreditsDelta / len(decision.Refs)
		}
		for _, ref := range decision.Refs {
			if _, err = tx.Exec(ctx, `INSERT INTO billing_ledger(user_id,delta,reason,action,ref_type,ref_id) VALUES($1,$2,$3,$4,'reservation',$5)`,
				userID, unit, decision.LedgerReason, decision.Action, ref); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func bumpUsage(ctx context.Context, tx pgx.Tx, userID string, bucket time.Time, action string, delta int) error {
	if delta == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO billing_usage(user_id,bucket,action,count) VALUES($1,$2,$3,$4)
		ON CONFLICT (user_id,bucket,action) DO UPDATE SET count=billing_usage.count+$4`, userID, bucket, action, delta)
	return err
}

func usageBuckets(now time.Time) (day, hour, minute time.Time) {
	now = now.UTC()
	day = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	hour = now.Truncate(time.Hour)
	minute = now.Truncate(time.Minute)
	return
}

func (s *Store) RefundBilling(ctx context.Context, refType, refID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var entry domain.BillingLedgerEntry
	var action string
	err = tx.QueryRow(ctx, `SELECT id::text,user_id::text,delta,reason,action,ref_type,ref_id FROM billing_ledger WHERE ref_type=$1 AND ref_id=$2 AND reason IN ('reserve','welcome')`, refType, refID).
		Scan(&entry.ID, &entry.UserID, &entry.Delta, &entry.Reason, &action, &entry.RefType, &entry.RefID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var refunded bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM billing_ledger WHERE ref_type=$1 AND ref_id=$2 AND reason='refund')`, refType, refID).Scan(&refunded); err != nil {
		return err
	}
	if refunded {
		return nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO billing_wallets(user_id) VALUES($1) ON CONFLICT (user_id) DO NOTHING`, entry.UserID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT credits FROM billing_wallets WHERE user_id=$1 FOR UPDATE`, entry.UserID); err != nil {
		return err
	}
	creditBack := 0
	if entry.Delta < 0 {
		creditBack = -entry.Delta
	}
	restoreAnalysis := entry.Reason == domain.LedgerWelcome && action == domain.BillingActionAnalysis
	restorePlan := false
	if entry.Reason == domain.LedgerWelcome && action == domain.BillingActionLook {
		var remaining int
		if err = tx.QueryRow(ctx, `
			SELECT count(*) FROM billing_ledger l
			WHERE l.user_id=$1 AND l.reason='welcome' AND l.action='look'
			  AND NOT (l.ref_type=$2 AND l.ref_id=$3)
			  AND NOT EXISTS (SELECT 1 FROM billing_ledger r WHERE r.ref_type=l.ref_type AND r.ref_id=l.ref_id AND r.reason='refund')`,
			entry.UserID, refType, refID).Scan(&remaining); err != nil {
			return err
		}
		restorePlan = remaining == 0
	}
	if _, err = tx.Exec(ctx, `
		UPDATE billing_wallets SET
			credits=credits+$2,
			welcome_analysis_used=CASE WHEN $3 THEN false ELSE welcome_analysis_used END,
			welcome_plan_set_used=CASE WHEN $4 THEN false ELSE welcome_plan_set_used END,
			updated_at=now()
		WHERE user_id=$1`, entry.UserID, creditBack, restoreAnalysis, restorePlan); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO billing_ledger(user_id,delta,reason,action,ref_type,ref_id) VALUES($1,$2,'refund',$3,$4,$5)`,
		entry.UserID, creditBack, action, refType, refID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RelinkBillingRefs(ctx context.Context, fromRefs, toRefs []string) error {
	if len(fromRefs) != len(toRefs) {
		return errors.New("billing ref relink length mismatch")
	}
	for i, from := range fromRefs {
		if _, err := s.pool.Exec(ctx, `UPDATE billing_ledger SET ref_type='task',ref_id=$2 WHERE ref_type='reservation' AND ref_id=$1`, from, toRefs[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListUnrefundedFailedTaskCharges(ctx context.Context) ([]domain.BillingLedgerEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id::text,l.user_id::text,l.delta,l.reason,l.ref_type,l.ref_id,l.created_at
		FROM billing_ledger l
		JOIN tasks t ON t.id::text=l.ref_id
		WHERE l.ref_type='task' AND l.reason IN ('reserve','welcome') AND t.status='failed'
		  AND NOT EXISTS (SELECT 1 FROM billing_ledger r WHERE r.ref_type=l.ref_type AND r.ref_id=l.ref_id AND r.reason='refund')
		LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.BillingLedgerEntry, 0)
	for rows.Next() {
		var item domain.BillingLedgerEntry
		if err := rows.Scan(&item.ID, &item.UserID, &item.Delta, &item.Reason, &item.RefType, &item.RefID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateBillingOrder(ctx context.Context, row domain.BillingOrderRow) (domain.BillingOrderRow, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO billing_orders(user_id,sku_id,product_id,credits,amount_fen,out_trade_no,status)
		VALUES($1,$2,$3,$4,$5,$6,'created')
		RETURNING id::text,user_id::text,sku_id,product_id,credits,amount_fen,out_trade_no,wx_order_id,status,created_at,updated_at`,
		row.UserID, row.SKUID, row.ProductID, row.Credits, row.AmountFen, row.OutTradeNo).
		Scan(&row.ID, &row.UserID, &row.SKUID, &row.ProductID, &row.Credits, &row.AmountFen, &row.OutTradeNo, &row.WxOrderID, &row.Status, &row.CreatedAt, &row.UpdatedAt)
	return row, err
}

func (s *Store) GetBillingOrder(ctx context.Context, userID, orderID string) (domain.BillingOrderRow, error) {
	row, err := scanBillingOrder(s.pool.QueryRow(ctx, billingOrderSelect+` WHERE id=$1::uuid AND user_id=$2`, orderID, userID))
	return row, mapNotFound(err)
}

func (s *Store) GetBillingOrderByOutTradeNo(ctx context.Context, outTradeNo string) (domain.BillingOrderRow, error) {
	row, err := scanBillingOrder(s.pool.QueryRow(ctx, billingOrderSelect+` WHERE out_trade_no=$1`, outTradeNo))
	return row, mapNotFound(err)
}

const billingOrderSelect = `SELECT id::text,user_id::text,sku_id,product_id,credits,amount_fen,out_trade_no,wx_order_id,status,created_at,updated_at FROM billing_orders`

func scanBillingOrder(row interface{ Scan(dest ...any) error }) (domain.BillingOrderRow, error) {
	var item domain.BillingOrderRow
	err := row.Scan(&item.ID, &item.UserID, &item.SKUID, &item.ProductID, &item.Credits, &item.AmountFen, &item.OutTradeNo, &item.WxOrderID, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) FulfillBillingOrder(ctx context.Context, outTradeNo, wxOrderID string) (domain.BillingOrderRow, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.BillingOrderRow{}, false, err
	}
	defer tx.Rollback(ctx)
	row, err := scanBillingOrder(tx.QueryRow(ctx, billingOrderSelect+` WHERE out_trade_no=$1 FOR UPDATE`, outTradeNo))
	if err != nil {
		return domain.BillingOrderRow{}, false, mapNotFound(err)
	}
	if row.Status == domain.OrderFulfilled {
		return row, false, tx.Commit(ctx)
	}
	if row.Status != domain.OrderCreated && row.Status != domain.OrderPaid {
		return row, false, repository.ErrNotFound
	}
	if _, err = tx.Exec(ctx, `INSERT INTO billing_wallets(user_id) VALUES($1) ON CONFLICT (user_id) DO NOTHING`, row.UserID); err != nil {
		return domain.BillingOrderRow{}, false, err
	}
	if _, err = tx.Exec(ctx, `SELECT credits FROM billing_wallets WHERE user_id=$1 FOR UPDATE`, row.UserID); err != nil {
		return domain.BillingOrderRow{}, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE billing_wallets SET credits=credits+$2,updated_at=now() WHERE user_id=$1`, row.UserID, row.Credits); err != nil {
		return domain.BillingOrderRow{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO billing_ledger(user_id,delta,reason,action,ref_type,ref_id) VALUES($1,$2,'purchase','order','order',$3)`,
		row.UserID, row.Credits, row.ID); err != nil {
		return domain.BillingOrderRow{}, false, err
	}
	if err = scanBillingOrderInto(&row, tx.QueryRow(ctx, `
		UPDATE billing_orders SET status='fulfilled',wx_order_id=$2,updated_at=now() WHERE id=$1
		RETURNING id::text,user_id::text,sku_id,product_id,credits,amount_fen,out_trade_no,wx_order_id,status,created_at,updated_at`, row.ID, wxOrderID)); err != nil {
		return domain.BillingOrderRow{}, false, err
	}
	return row, true, tx.Commit(ctx)
}

func scanBillingOrderInto(row *domain.BillingOrderRow, scanner interface{ Scan(dest ...any) error }) error {
	return scanner.Scan(&row.ID, &row.UserID, &row.SKUID, &row.ProductID, &row.Credits, &row.AmountFen, &row.OutTradeNo, &row.WxOrderID, &row.Status, &row.CreatedAt, &row.UpdatedAt)
}
