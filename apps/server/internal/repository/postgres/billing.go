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

const reservationSelect = `
	SELECT id::text, user_id::text, operation_id::text, product, units, charge_source, status,
	       publication_id::text, created_at, settled_at, refunded_at
	FROM billing_reservations`

const reservationReturning = `
	RETURNING id::text, user_id::text, operation_id::text, product, units, charge_source, status,
	          publication_id::text, created_at, settled_at, refunded_at`

// Reserve charges a not-yet-terminal operation by reserving either credits or a
// welcome entitlement, then writes the reserve ledger entry. It is idempotent
// per operation: a replay of the same product/units returns the existing
// reservation without a second deduction.
func (s *Store) Reserve(ctx context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Reservation{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO billing_wallets(user_id) VALUES($1::uuid) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return domain.Reservation{}, err
	}

	var opStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1::uuid AND user_id=$2::uuid FOR UPDATE`, operationID, userID).Scan(&opStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Reservation{}, repository.ErrNotFound
	}
	if err != nil {
		return domain.Reservation{}, err
	}
	if opStatus != string(domain.OperationAccepted) && opStatus != string(domain.OperationRunning) && opStatus != string(domain.OperationRetrying) {
		return domain.Reservation{}, repository.ErrConflict
	}

	existing, err := scanReservation(tx.QueryRow(ctx, reservationSelect+` WHERE user_id=$1::uuid AND operation_id=$2::uuid`, userID, operationID))
	if err == nil {
		if existing.Product == product && existing.Units == units {
			return existing, tx.Commit(ctx)
		}
		return domain.Reservation{}, domain.ErrReservationConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Reservation{}, err
	}

	var credits int
	var welcomeAnalysis, welcomePlanSet bool
	if err := tx.QueryRow(ctx, `SELECT credits, welcome_analysis_used, welcome_plan_set_used FROM billing_wallets WHERE user_id=$1::uuid FOR UPDATE`, userID).
		Scan(&credits, &welcomeAnalysis, &welcomePlanSet); err != nil {
		return domain.Reservation{}, err
	}

	chargeSource := chargeSourceFor(product, welcomeAnalysis, welcomePlanSet)
	delta := 0
	switch chargeSource {
	case domain.ChargeCredits:
		if credits < units {
			return domain.Reservation{}, domain.ErrInsufficientCredits
		}
		delta = -units
		if _, err := tx.Exec(ctx, `UPDATE billing_wallets SET credits=credits-$2, version=version+1, updated_at=now() WHERE user_id=$1::uuid`, userID, units); err != nil {
			return domain.Reservation{}, err
		}
	case domain.ChargeWelcomeAnalysis:
		if _, err := tx.Exec(ctx, `UPDATE billing_wallets SET welcome_analysis_used=true, version=version+1, updated_at=now() WHERE user_id=$1::uuid`, userID); err != nil {
			return domain.Reservation{}, err
		}
	case domain.ChargeWelcomePlanSet:
		if _, err := tx.Exec(ctx, `UPDATE billing_wallets SET welcome_plan_set_used=true, version=version+1, updated_at=now() WHERE user_id=$1::uuid`, userID); err != nil {
			return domain.Reservation{}, err
		}
	}

	reserved, err := scanReservation(tx.QueryRow(ctx, `
		INSERT INTO billing_reservations(user_id, operation_id, product, units, charge_source, status)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'reserved')`+reservationReturning,
		userID, operationID, product, units, chargeSource))
	if err != nil {
		return domain.Reservation{}, err
	}

	if err := insertOperationLedger(ctx, tx, userID, domain.LedgerReserve, product, chargeSource, delta, operationID, nil); err != nil {
		return domain.Reservation{}, err
	}
	return reserved, tx.Commit(ctx)
}

// Settle marks a reserved operation's charge as consumed once the operation
// succeeded with a result matching its product. A render publication must also
// belong to this operation's run.
func (s *Store) Settle(ctx context.Context, userID, operationID string, publicationID *string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	existing, err := scanReservation(tx.QueryRow(ctx, reservationSelect+`
		WHERE user_id=$1::uuid AND operation_id=$2::uuid FOR UPDATE`, userID, operationID))
	if err != nil {
		return mapNotFound(err)
	}
	switch existing.Status {
	case domain.BillingSettled:
		return tx.Commit(ctx)
	case domain.BillingRefunded:
		return domain.ErrAlreadyRefunded
	}

	status, resultType, resultID, err := lockOperationResult(ctx, tx, userID, operationID)
	if err != nil {
		return err
	}
	if status != string(domain.OperationSucceeded) {
		return repository.ErrConflict
	}
	if err := settleReserved(ctx, tx, existing, derefString(resultType), resultID, publicationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Refund returns the reserved charge once the operation failed, was cancelled
// or superseded. It is idempotent: a second refund of the same reservation is a
// no-op.
func (s *Store) Refund(ctx context.Context, userID, operationID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	existing, err := scanReservation(tx.QueryRow(ctx, reservationSelect+`
		WHERE user_id=$1::uuid AND operation_id=$2::uuid FOR UPDATE`, userID, operationID))
	if err != nil {
		return mapNotFound(err)
	}
	switch existing.Status {
	case domain.BillingRefunded:
		return tx.Commit(ctx)
	case domain.BillingSettled:
		return domain.ErrAlreadySettled
	}

	status, _, _, err := lockOperationResult(ctx, tx, userID, operationID)
	if err != nil {
		return err
	}
	switch status {
	case string(domain.OperationFailed), string(domain.OperationCancelled), string(domain.OperationSuperseded):
	default:
		return repository.ErrConflict
	}
	if err := refundReserved(ctx, tx, existing); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReconcileTerminalOperations settles or refunds reservations whose operation
// reached a terminal state but whose reservation was never finalised. It
// processes at most limit rows (default 100), skipping any concurrently locked
// by another worker, and reports per-operation failures rather than aborting
// the sweep.
func (s *Store) ReconcileTerminalOperations(ctx context.Context, limit int) (domain.ReconcileResult, error) {
	if limit <= 0 {
		limit = 100
	}
	result := domain.ReconcileResult{Failed: []domain.ReconcileFailure{}}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT r.id::text, r.user_id::text, r.operation_id::text, r.product, r.units, r.charge_source, r.status,
		       r.publication_id::text, r.created_at, r.settled_at, r.refunded_at,
		       o.status, o.result_type, o.result_id::text
		FROM billing_reservations r
		JOIN operations o ON o.user_id=r.user_id AND o.id=r.operation_id
		WHERE r.status='reserved' AND o.status IN ('succeeded','failed','cancelled','superseded')
		ORDER BY r.created_at
		FOR UPDATE OF r SKIP LOCKED
		LIMIT $1`, limit)
	if err != nil {
		return result, err
	}

	// Materialise the locked rows before writing anything: the settle/refund
	// helpers execute their own queries on the same transaction, which would
	// fail with "conn busy" while the SELECT cursor is still open. The FOR
	// UPDATE locks persist until commit regardless of cursor lifetime.
	type pending struct {
		reservation  domain.Reservation
		opStatus     string
		opResultType string
		opResultID   *string
	}
	items := make([]pending, 0, 16)
	for rows.Next() {
		var p pending
		var publicationID *string
		var settledAt, refundedAt *time.Time
		var opResultType, opResultID *string
		if err := rows.Scan(
			&p.reservation.ID, &p.reservation.UserID, &p.reservation.OperationID, &p.reservation.Product, &p.reservation.Units,
			&p.reservation.ChargeSource, &p.reservation.Status, &publicationID, &p.reservation.CreatedAt, &settledAt, &refundedAt,
			&p.opStatus, &opResultType, &opResultID); err != nil {
			rows.Close()
			return result, err
		}
		p.reservation.PublicationID = publicationID
		p.reservation.SettledAt = settledAt
		p.reservation.RefundedAt = refundedAt
		p.opResultType = derefString(opResultType)
		p.opResultID = opResultID
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	for _, p := range items {
		var err error
		switch p.opStatus {
		case string(domain.OperationSucceeded):
			var pubID *string
			if p.reservation.Product == domain.ProductRenderPublication {
				pubID = p.opResultID
			}
			err = settleReserved(ctx, tx, p.reservation, p.opResultType, p.opResultID, pubID)
			if err == nil {
				result.Settled++
			}
		default:
			err = refundReserved(ctx, tx, p.reservation)
			if err == nil {
				result.Refunded++
			}
		}
		if err != nil {
			result.Failed = append(result.Failed, domain.ReconcileFailure{OperationID: p.reservation.OperationID, Err: err})
		}
	}
	return result, tx.Commit(ctx)
}

// settleReserved finalises a single reserved reservation whose operation is
// succeeded. It validates the operation result against the product, verifies a
// render publication belongs to the operation's run, then writes the settle
// ledger entry.
func settleReserved(ctx context.Context, tx pgx.Tx, reservation domain.Reservation, opResultType string, opResultID, publicationID *string) error {
	if err := validateSettleResult(reservation.Product, opResultType, opResultID, publicationID); err != nil {
		return err
	}
	if reservation.Product == domain.ProductRenderPublication && !publicationBelongsToRun(ctx, tx, reservation.UserID, *publicationID, reservation.OperationID) {
		return repository.ErrConflict
	}
	if _, err := tx.Exec(ctx, `
		UPDATE billing_reservations
		SET status='settled', publication_id=$3::uuid, settled_at=now()
		WHERE user_id=$1::uuid AND operation_id=$2::uuid AND status='reserved'`,
		reservation.UserID, reservation.OperationID, publicationID); err != nil {
		return err
	}
	return insertOperationLedger(ctx, tx, reservation.UserID, domain.LedgerSettle, reservation.Product, reservation.ChargeSource, 0, reservation.OperationID, publicationID)
}

// refundReserved finalises a single reserved reservation whose operation ended
// in a non-success terminal state: it returns credits or restores the welcome
// flag and writes the refund ledger entry.
func refundReserved(ctx context.Context, tx pgx.Tx, reservation domain.Reservation) error {
	if _, err := tx.Exec(ctx, `
		UPDATE billing_reservations
		SET status='refunded', refunded_at=now()
		WHERE user_id=$1::uuid AND operation_id=$2::uuid AND status='reserved'`,
		reservation.UserID, reservation.OperationID); err != nil {
		return err
	}
	switch reservation.ChargeSource {
	case domain.ChargeCredits:
		if _, err := tx.Exec(ctx, `UPDATE billing_wallets SET credits=credits+$2, version=version+1, updated_at=now() WHERE user_id=$1::uuid`, reservation.UserID, reservation.Units); err != nil {
			return err
		}
		return insertOperationLedger(ctx, tx, reservation.UserID, domain.LedgerRefund, reservation.Product, reservation.ChargeSource, reservation.Units, reservation.OperationID, nil)
	case domain.ChargeWelcomeAnalysis:
		if _, err := tx.Exec(ctx, `UPDATE billing_wallets SET welcome_analysis_used=false, version=version+1, updated_at=now() WHERE user_id=$1::uuid`, reservation.UserID); err != nil {
			return err
		}
		return insertOperationLedger(ctx, tx, reservation.UserID, domain.LedgerRefund, reservation.Product, reservation.ChargeSource, 0, reservation.OperationID, nil)
	case domain.ChargeWelcomePlanSet:
		if _, err := tx.Exec(ctx, `UPDATE billing_wallets SET welcome_plan_set_used=false, version=version+1, updated_at=now() WHERE user_id=$1::uuid`, reservation.UserID); err != nil {
			return err
		}
		return insertOperationLedger(ctx, tx, reservation.UserID, domain.LedgerRefund, reservation.Product, reservation.ChargeSource, 0, reservation.OperationID, nil)
	default:
		return nil
	}
}

// lockOperationResult locks the operation row and returns its status and
// nullable result fields.
func lockOperationResult(ctx context.Context, tx pgx.Tx, userID, operationID string) (status string, resultType, resultID *string, err error) {
	err = tx.QueryRow(ctx, `
		SELECT status, result_type, result_id::text
		FROM operations WHERE id=$1::uuid AND user_id=$2::uuid FOR UPDATE`,
		operationID, userID).Scan(&status, &resultType, &resultID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil, repository.ErrNotFound
	}
	return status, resultType, resultID, err
}

// chargeSourceFor picks the welcome entitlement a first-time assessment or plan
// set may consume, falling back to credits when already used or for renders.
func chargeSourceFor(product domain.Product, welcomeAnalysisUsed, welcomePlanSetUsed bool) domain.ChargeSource {
	switch product {
	case domain.ProductAssessment:
		if !welcomeAnalysisUsed {
			return domain.ChargeWelcomeAnalysis
		}
	case domain.ProductPlanSet:
		if !welcomePlanSetUsed {
			return domain.ChargeWelcomePlanSet
		}
	}
	return domain.ChargeCredits
}

func expectedResultType(product domain.Product) string {
	switch product {
	case domain.ProductAssessment:
		return "report"
	case domain.ProductPlanSet:
		return "plan_set"
	case domain.ProductRenderPublication:
		return "render_publication"
	default:
		return ""
	}
}

// validateSettleResult enforces that the operation's terminal result matches
// the reserved product: assessment→report, plan_set→plan_set, and
// render→render_publication whose publication id equals the operation result.
func validateSettleResult(product domain.Product, opResultType string, opResultID, publicationID *string) error {
	if opResultType != expectedResultType(product) {
		return repository.ErrConflict
	}
	if opResultID == nil {
		return repository.ErrConflict
	}
	if product == domain.ProductRenderPublication {
		if publicationID == nil || *publicationID != *opResultID {
			return repository.ErrConflict
		}
	} else if publicationID != nil {
		return repository.ErrConflict
	}
	return nil
}

// publicationBelongsToRun verifies a render publication belongs to a run whose
// operation is operationID, closing the gap between the publication and the
// billed operation.
func publicationBelongsToRun(ctx context.Context, tx pgx.Tx, userID, publicationID, operationID string) bool {
	var owned bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM render_publications p
			JOIN render_runs r ON r.user_id=p.user_id AND r.id=p.render_run_id
			WHERE p.user_id=$1::uuid AND p.id=$2::uuid AND r.operation_id=$3::uuid
		)`, userID, publicationID, operationID).Scan(&owned)
	return err == nil && owned
}

// insertOperationLedger writes one operation-bound billing ledger entry. The
// publication id is only set for render settles.
func insertOperationLedger(ctx context.Context, tx pgx.Tx, userID, entryType string, product domain.Product, chargeSource domain.ChargeSource, delta int, operationID string, publicationID *string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO billing_ledger(user_id, entry_type, product, charge_source, delta, operation_id, publication_id)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7::uuid)`,
		userID, entryType, product, chargeSource, delta, operationID, publicationID)
	return err
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func scanReservation(row interface{ Scan(dest ...any) error }) (domain.Reservation, error) {
	var item domain.Reservation
	var publicationID *string
	var settledAt, refundedAt *time.Time
	err := row.Scan(
		&item.ID, &item.UserID, &item.OperationID, &item.Product, &item.Units,
		&item.ChargeSource, &item.Status, &publicationID, &item.CreatedAt, &settledAt, &refundedAt,
	)
	item.PublicationID = publicationID
	item.SettledAt = settledAt
	item.RefundedAt = refundedAt
	return item, err
}
