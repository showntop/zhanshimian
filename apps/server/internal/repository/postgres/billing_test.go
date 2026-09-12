package postgres

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestReserveWelcomeAssessmentIsFree(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	reservation, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductAssessment, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ChargeSource != domain.ChargeWelcomeAnalysis {
		t.Fatalf("charge_source = %s, want welcome_analysis", reservation.ChargeSource)
	}
	if reservation.Status != domain.BillingReserved {
		t.Fatalf("status = %s", reservation.Status)
	}
	if got := walletCredits(t, pool, ids.userID); got != 5 {
		t.Fatalf("credits = %d, want 5 (welcome is free)", got)
	}
}

func TestReservePlanSetUsesWelcomePlanSet(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	reservation, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductPlanSet, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ChargeSource != domain.ChargeWelcomePlanSet {
		t.Fatalf("charge_source = %s, want welcome_plan_set", reservation.ChargeSource)
	}
	if got := walletCredits(t, pool, ids.userID); got != 5 {
		t.Fatalf("credits = %d, want 5 (welcome is free)", got)
	}
}

func TestReserveAssessmentFallsBackToCreditsAfterWelcomeUsed(t *testing.T) {
	store, pool, ids := newBillingRepoWithFlags(t, 5, true, false)
	reservation, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductAssessment, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ChargeSource != domain.ChargeCredits {
		t.Fatalf("charge_source = %s, want credits", reservation.ChargeSource)
	}
	if got := walletCredits(t, pool, ids.userID); got != 4 {
		t.Fatalf("credits = %d, want 4", got)
	}
}

func TestReserveRenderInsufficientCreditsChangesNoRow(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 0)
	_, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1)
	if !errors.Is(err, domain.ErrInsufficientCredits) {
		t.Fatalf("Reserve error = %v, want ErrInsufficientCredits", err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 0 {
		t.Fatalf("credits = %d, want 0", got)
	}
	if n := countReservations(t, pool, ids.userID); n != 0 {
		t.Fatalf("reservations = %d, want 0", n)
	}
	if n := countLedger(t, pool, ids.userID); n != 0 {
		t.Fatalf("ledger rows = %d, want 0", n)
	}
}

// 未开通支付时（WithSkipCreditCharge）次数不足不拦生成：reserve 照常受理，
// 台账记 0 扣减；后续退款也不得凭空返次数（31916c1 移植自旧计费）。
func TestReserveInsufficientCreditsAllowedWhenCreditChargeSkipped(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 0)
	gated := New(pool, WithSkipCreditCharge(true))
	reservation, err := gated.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ChargeSource != domain.ChargeCredits {
		t.Fatalf("charge_source = %s, want credits", reservation.ChargeSource)
	}
	if got := walletCredits(t, pool, ids.userID); got != 0 {
		t.Fatalf("credits = %d, want 0 (no deduction when charge skipped)", got)
	}
	entry := loadLedger(t, pool, ids.userID, domain.LedgerReserve)
	if entry.delta != 0 || entry.operationID != ids.opID {
		t.Fatalf("reserve ledger = %+v, want delta 0 for skipped charge", entry)
	}
	_ = store
}

func TestRefundAfterSkippedChargeAddsNoCredits(t *testing.T) {
	_, pool, ids := newBillingRepo(t, 0)
	gated := New(pool, WithSkipCreditCharge(true))
	if _, err := gated.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	markOperationFailed(t, gated, ids.userID, ids.opID)
	if err := gated.Refund(context.Background(), ids.userID, ids.opID); err != nil {
		t.Fatal(err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 0 {
		t.Fatalf("credits = %d, want 0 (refund must not mint credits for a skipped charge)", got)
	}
	entry := loadLedger(t, pool, ids.userID, domain.LedgerRefund)
	if entry.delta != 0 {
		t.Fatalf("refund ledger = %+v, want delta 0", entry)
	}
}

func TestReserveRenderDeductsCredits(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	reservation, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.ChargeSource != domain.ChargeCredits {
		t.Fatalf("charge_source = %s, want credits", reservation.ChargeSource)
	}
	if got := walletCredits(t, pool, ids.userID); got != 4 {
		t.Fatalf("credits = %d, want 4", got)
	}
	entry := loadLedger(t, pool, ids.userID, domain.LedgerReserve)
	if entry.delta != -1 || entry.operationID != ids.opID || entry.product != string(domain.ProductRenderPublication) {
		t.Fatalf("reserve ledger = %+v", entry)
	}
}

func TestReserveIdempotentSameProductAndUnits(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	first, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay ids differ: %s vs %s", first.ID, second.ID)
	}
	if got := walletCredits(t, pool, ids.userID); got != 4 {
		t.Fatalf("credits = %d, want 4 (deducted once)", got)
	}
	if n := countLedgerByType(t, pool, ids.userID, domain.LedgerReserve); n != 1 {
		t.Fatalf("reserve ledger rows = %d, want 1", n)
	}
}

func TestReserveConflictOnDifferentProductOrUnits(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductAssessment, 1); !errors.Is(err, domain.ErrReservationConflict) {
		t.Fatalf("product mismatch error = %v, want ErrReservationConflict", err)
	}
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 2); !errors.Is(err, domain.ErrReservationConflict) {
		t.Fatalf("units mismatch error = %v, want ErrReservationConflict", err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 4 {
		t.Fatalf("credits after conflict = %d, want 4", got)
	}
}

func TestReserveMissingAndCrossUserNotFound(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 3)
	otherID := insertUser(t, store)
	foreign := seedOperation(t, store, otherID, domain.Operation{
		Kind: domain.OperationRender, Status: domain.OperationAccepted,
	})
	if _, err := store.Reserve(context.Background(), ids.userID, uuid.NewString(), domain.ProductRenderPublication, 1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing operation error = %v, want ErrNotFound", err)
	}
	if _, err := store.Reserve(context.Background(), ids.userID, foreign.ID, domain.ProductRenderPublication, 1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user operation error = %v, want ErrNotFound", err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 3 {
		t.Fatalf("credits after not-found = %d, want 3", got)
	}
	if n := countReservations(t, pool, ids.userID); n != 0 {
		t.Fatalf("reservations = %d, want 0", n)
	}
}

func TestReserveRejectsTerminalOperation(t *testing.T) {
	store, _, ids := newBillingRepo(t, 3)
	if _, err := store.pool.Exec(context.Background(), `
		UPDATE operations SET status='succeeded', result_type='report', result_id=$3::uuid
		WHERE user_id=$1::uuid AND id=$2::uuid`, ids.userID, ids.opID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductAssessment, 1); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("terminal operation error = %v, want ErrConflict", err)
	}
}

func TestSettleAssessmentIdempotent(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	reportID := uuid.NewString()
	op := seedOperation(t, store, ids.userID, domain.Operation{Kind: domain.OperationAssessment, Status: domain.OperationAccepted})
	if _, err := store.Reserve(context.Background(), ids.userID, op.ID, domain.ProductAssessment, 1); err != nil {
		t.Fatal(err)
	}
	markOperationSucceeded(t, store, ids.userID, op.ID, "report", reportID)
	if err := store.Settle(context.Background(), ids.userID, op.ID, nil); err != nil {
		t.Fatalf("first Settle: %v", err)
	}
	if err := store.Settle(context.Background(), ids.userID, op.ID, nil); err != nil {
		t.Fatalf("idempotent Settle: %v", err)
	}
	row := loadReservation(t, pool, ids.userID, op.ID)
	if row.status != string(domain.BillingSettled) {
		t.Fatalf("status = %s, want settled", row.status)
	}
	entry := loadLedger(t, pool, ids.userID, domain.LedgerSettle)
	if entry.delta != 0 || entry.operationID != op.ID {
		t.Fatalf("settle ledger = %+v", entry)
	}
	if n := countLedgerByType(t, pool, ids.userID, domain.LedgerSettle); n != 1 {
		t.Fatalf("settle ledger rows = %d, want 1", n)
	}
}

func TestSettleRequiresSucceededOperation(t *testing.T) {
	store, _, ids := newBillingRepo(t, 5)
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductAssessment, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Settle(context.Background(), ids.userID, ids.opID, nil); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("settle while running error = %v, want ErrConflict", err)
	}
}

func TestSettleRejectsWrongResultType(t *testing.T) {
	store, _, ids := newBillingRepo(t, 5)
	op := seedOperation(t, store, ids.userID, domain.Operation{Kind: domain.OperationAssessment, Status: domain.OperationAccepted})
	if _, err := store.Reserve(context.Background(), ids.userID, op.ID, domain.ProductAssessment, 1); err != nil {
		t.Fatal(err)
	}
	markOperationSucceeded(t, store, ids.userID, op.ID, "plan_set", uuid.NewString())
	if err := store.Settle(context.Background(), ids.userID, op.ID, nil); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("wrong result_type error = %v, want ErrConflict", err)
	}
}

func TestSettleRenderRequiresPublication(t *testing.T) {
	store, _, ids := newBillingRepo(t, 5)
	op := seedOperation(t, store, ids.userID, domain.Operation{Kind: domain.OperationRender, Status: domain.OperationAccepted})
	if _, err := store.Reserve(context.Background(), ids.userID, op.ID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	markOperationSucceeded(t, store, ids.userID, op.ID, "render_publication", uuid.NewString())
	if err := store.Settle(context.Background(), ids.userID, op.ID, nil); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("settle render without publication error = %v, want ErrConflict", err)
	}
}

func TestSettleRejectsRefundedReservation(t *testing.T) {
	store, _, ids := newBillingRepo(t, 5)
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	markOperationFailed(t, store, ids.userID, ids.opID)
	if err := store.Refund(context.Background(), ids.userID, ids.opID); err != nil {
		t.Fatal(err)
	}
	if err := store.Settle(context.Background(), ids.userID, ids.opID, nil); !errors.Is(err, domain.ErrAlreadyRefunded) {
		t.Fatalf("settle after refund error = %v, want ErrAlreadyRefunded", err)
	}
}

func TestRefundRestoresCreditsAndIsIdempotent(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	markOperationFailed(t, store, ids.userID, ids.opID)
	if err := store.Refund(context.Background(), ids.userID, ids.opID); err != nil {
		t.Fatalf("first Refund: %v", err)
	}
	if err := store.Refund(context.Background(), ids.userID, ids.opID); err != nil {
		t.Fatalf("idempotent Refund: %v", err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 5 {
		t.Fatalf("credits after refund = %d, want 5", got)
	}
	if n := countLedgerByType(t, pool, ids.userID, domain.LedgerRefund); n != 1 {
		t.Fatalf("refund ledger rows = %d, want 1", n)
	}
}

func TestRefundRejectsSettledReservation(t *testing.T) {
	store, _, ids := newBillingRepo(t, 5)
	op := seedOperation(t, store, ids.userID, domain.Operation{Kind: domain.OperationAssessment, Status: domain.OperationAccepted})
	if _, err := store.Reserve(context.Background(), ids.userID, op.ID, domain.ProductAssessment, 1); err != nil {
		t.Fatal(err)
	}
	markOperationSucceeded(t, store, ids.userID, op.ID, "report", uuid.NewString())
	if err := store.Settle(context.Background(), ids.userID, op.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Refund(context.Background(), ids.userID, op.ID); !errors.Is(err, domain.ErrAlreadySettled) {
		t.Fatalf("refund after settle error = %v, want ErrAlreadySettled", err)
	}
}

func TestRefundRejectsNonTerminalOperation(t *testing.T) {
	store, _, ids := newBillingRepo(t, 5)
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Refund(context.Background(), ids.userID, ids.opID); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("refund while running error = %v, want ErrConflict", err)
	}
}

func TestReserveConcurrentDeductsOnce(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 20)
	var successes atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
				errs <- err
			} else {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// Every caller replays the same reservation idempotently, so all succeed
	// while exactly one credit is deducted.
	if successes.Load() != 20 {
		t.Fatalf("successes = %d, want 20", successes.Load())
	}
	if got := walletCredits(t, pool, ids.userID); got != 19 {
		t.Fatalf("credits after concurrent reserve = %d, want 19", got)
	}
	if n := countLedgerByType(t, pool, ids.userID, domain.LedgerReserve); n != 1 {
		t.Fatalf("reserve ledger rows = %d, want 1", n)
	}
}

func TestRefundConcurrentCreditsOnce(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	if _, err := store.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	markOperationFailed(t, store, ids.userID, ids.opID)
	var successes atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Refund(context.Background(), ids.userID, ids.opID); err != nil {
				errs <- err
			} else {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// Every caller replays the refund idempotently, so all succeed while the
	// credit is returned exactly once.
	if successes.Load() != 20 {
		t.Fatalf("successes = %d, want 20", successes.Load())
	}
	if got := walletCredits(t, pool, ids.userID); got != 5 {
		t.Fatalf("credits after concurrent refund = %d, want 5", got)
	}
	if n := countLedgerByType(t, pool, ids.userID, domain.LedgerRefund); n != 1 {
		t.Fatalf("refund ledger rows = %d, want 1", n)
	}
}

func TestReconcileSettlesSucceededAndRefundsFailed(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)

	succeededOp := seedOperation(t, store, ids.userID, domain.Operation{Kind: domain.OperationAssessment, Status: domain.OperationAccepted})
	if _, err := store.Reserve(context.Background(), ids.userID, succeededOp.ID, domain.ProductAssessment, 1); err != nil {
		t.Fatal(err)
	}
	markOperationSucceeded(t, store, ids.userID, succeededOp.ID, "report", uuid.NewString())

	failedOp := seedOperation(t, store, ids.userID, domain.Operation{Kind: domain.OperationRender, Status: domain.OperationAccepted})
	if _, err := store.Reserve(context.Background(), ids.userID, failedOp.ID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatal(err)
	}
	markOperationFailed(t, store, ids.userID, failedOp.ID)

	result, err := store.ReconcileTerminalOperations(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Settled != 1 || result.Refunded != 1 {
		t.Fatalf("reconcile = settled %d refunded %d, want 1/1", result.Settled, result.Refunded)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("reconcile failures = %#v", result.Failed)
	}
	// The failed render reservation returned its credit.
	if got := walletCredits(t, pool, ids.userID); got != 5 {
		t.Fatalf("credits after reconcile = %d, want 5", got)
	}
	if status := loadReservation(t, pool, ids.userID, succeededOp.ID).status; status != string(domain.BillingSettled) {
		t.Fatalf("succeeded reservation status = %s, want settled", status)
	}
	if status := loadReservation(t, pool, ids.userID, failedOp.ID).status; status != string(domain.BillingRefunded) {
		t.Fatalf("failed reservation status = %s, want refunded", status)
	}
}

type billingIDs struct {
	userID string
	opID   string
}

func newBillingRepo(t *testing.T, credits int) (*Store, *pgxpool.Pool, billingIDs) {
	return newBillingRepoWithFlags(t, credits, false, false)
}

func newBillingRepoWithFlags(t *testing.T, credits int, welcomeAnalysisUsed, welcomePlanSetUsed bool) (*Store, *pgxpool.Pool, billingIDs) {
	t.Helper()
	pool := testutil.NewPostgres(t)
	store := New(pool)
	userID := insertUser(t, store)
	op := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationRender, Status: domain.OperationAccepted,
	})
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO billing_wallets(user_id, credits, welcome_analysis_used, welcome_plan_set_used) VALUES($1::uuid, $2, $3, $4)`,
		userID, credits, welcomeAnalysisUsed, welcomePlanSetUsed); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return store, pool, billingIDs{userID: userID, opID: op.ID}
}

func markOperationSucceeded(t *testing.T, store *Store, userID, opID, resultType, resultID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `
		UPDATE operations SET status='succeeded', result_type=$3, result_id=$4::uuid, finished_at=now(), updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, opID, resultType, resultID); err != nil {
		t.Fatalf("mark operation succeeded: %v", err)
	}
}

func markOperationFailed(t *testing.T, store *Store, userID, opID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `
		UPDATE operations SET status='failed', trace_id='test-fail', finished_at=now(), updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, opID); err != nil {
		t.Fatalf("mark operation failed: %v", err)
	}
}

func walletCredits(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var credits int
	if err := pool.QueryRow(context.Background(),
		`SELECT credits FROM billing_wallets WHERE user_id=$1::uuid`, userID).Scan(&credits); err != nil {
		t.Fatal(err)
	}
	return credits
}

func countReservations(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM billing_reservations WHERE user_id=$1::uuid`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countLedger(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM billing_ledger WHERE user_id=$1::uuid`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countLedgerByType(t *testing.T, pool *pgxpool.Pool, userID, entryType string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM billing_ledger WHERE user_id=$1::uuid AND entry_type=$2`, userID, entryType).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type ledgerRow struct {
	delta        int
	product      string
	chargeSource string
	operationID  string
}

func loadLedger(t *testing.T, pool *pgxpool.Pool, userID, entryType string) ledgerRow {
	t.Helper()
	var row ledgerRow
	var operationID *string
	err := pool.QueryRow(context.Background(), `
		SELECT delta, product, charge_source, operation_id::text
		FROM billing_ledger WHERE user_id=$1::uuid AND entry_type=$2`, userID, entryType).
		Scan(&row.delta, &row.product, &row.chargeSource, &operationID)
	if err != nil {
		t.Fatalf("load ledger %s: %v", entryType, err)
	}
	if operationID != nil {
		row.operationID = *operationID
	}
	return row
}

type reservationRow struct {
	status string
}

func loadReservation(t *testing.T, pool *pgxpool.Pool, userID, opID string) reservationRow {
	t.Helper()
	var row reservationRow
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM billing_reservations WHERE user_id=$1::uuid AND operation_id=$2::uuid`,
		userID, opID).Scan(&row.status); err != nil {
		t.Fatal(err)
	}
	return row
}
