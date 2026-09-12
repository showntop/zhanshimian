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
	"github.com/zhanshimian/server/internal/service/billing"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestReservationInsufficientCreditsChangesNoRow(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 0)
	_, created, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
	})
	if !errors.Is(err, billing.ErrInsufficientCredits) {
		t.Fatalf("Reserve error = %v, want ErrInsufficientCredits", err)
	}
	if created {
		t.Fatal("insufficient Reserve created=true")
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

func TestReservationConflictOnDifferentKindOrUnits(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 5)
	first, created, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
	})
	if err != nil || !created {
		t.Fatalf("first Reserve: created=%v err=%v", created, err)
	}
	_, _, err = store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "assessment", Units: 1,
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("kind mismatch error = %v, want ErrConflict", err)
	}
	_, _, err = store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 2,
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("units mismatch error = %v, want ErrConflict", err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 4 {
		t.Fatalf("credits after conflict = %d, want 4", got)
	}
	if first.Kind != "render" || first.Units != 1 {
		t.Fatalf("existing reservation mutated: %#v", first)
	}
}

func TestReservationMissingAndCrossUserNotFound(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 3)
	otherID := insertUser(t, store)
	foreign := seedOperation(t, store, otherID, domain.Operation{
		Kind: domain.OperationRender, Status: domain.OperationAccepted,
	})
	_, _, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: uuid.NewString(), Kind: "render", Units: 1,
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing operation error = %v, want ErrNotFound", err)
	}
	_, _, err = store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: foreign.ID, Kind: "render", Units: 1,
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user operation error = %v, want ErrNotFound", err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 3 {
		t.Fatalf("credits after not-found = %d, want 3", got)
	}
	if n := countReservations(t, pool, ids.userID); n != 0 {
		t.Fatalf("reservations = %d, want 0", n)
	}
}

func TestSettleIdempotentSameResultAndRejectsMismatch(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 2)
	if _, _, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
	}); err != nil {
		t.Fatal(err)
	}
	first, settled, err := store.Settle(context.Background(), domain.SettleBilling{
		UserID: ids.userID, OperationID: ids.opID,
		ResultType: "render_publication", ResultID: ids.resultID,
	})
	if err != nil || !settled {
		t.Fatalf("first Settle: settled=%v err=%v", settled, err)
	}
	if first.ResultType != "render_publication" || first.ResultID != ids.resultID {
		t.Fatalf("settled result = %#v", first)
	}
	second, settled, err := store.Settle(context.Background(), domain.SettleBilling{
		UserID: ids.userID, OperationID: ids.opID,
		ResultType: "render_publication", ResultID: ids.resultID,
	})
	if err != nil {
		t.Fatalf("idempotent Settle: %v", err)
	}
	if settled {
		t.Fatal("second Settle settled=true, want false")
	}
	if second.ID != first.ID {
		t.Fatalf("settle IDs differ: %s vs %s", first.ID, second.ID)
	}
	_, _, err = store.Settle(context.Background(), domain.SettleBilling{
		UserID: ids.userID, OperationID: ids.opID,
		ResultType: "render_publication", ResultID: uuid.NewString(),
	})
	if err == nil {
		t.Fatal("different result Settle succeeded")
	}
	entry := loadLedger(t, pool, ids.userID, "settle")
	if entry.delta != 0 || entry.refType != "operation" || entry.refID != ids.opID {
		t.Fatalf("settle ledger = %+v", entry)
	}
	if countLedgerByReason(t, pool, ids.userID, "settle") != 1 {
		t.Fatal("expected one settle ledger row")
	}
}

func TestSettleRejectsRefundedReservation(t *testing.T) {
	store, _, ids := newBillingRepo(t, 1)
	if _, _, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Refund(context.Background(), domain.RefundBilling{
		UserID: ids.userID, OperationID: ids.opID, Reason: "superseded",
	}); err != nil {
		t.Fatal(err)
	}
	_, settled, err := store.Settle(context.Background(), domain.SettleBilling{
		UserID: ids.userID, OperationID: ids.opID,
		ResultType: "render_publication", ResultID: ids.resultID,
	})
	if err == nil || settled {
		t.Fatalf("settle after refund: settled=%v err=%v", settled, err)
	}
}

func TestRefundRejectsInvalidReasonAndLeavesRowsUnchanged(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 1)
	if _, _, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "assessment", Units: 1,
	}); err != nil {
		t.Fatal(err)
	}
	_, changed, err := store.Refund(context.Background(), domain.RefundBilling{
		UserID: ids.userID, OperationID: ids.opID, Reason: "timeout",
	})
	if err == nil || changed {
		t.Fatalf("invalid refund: changed=%v err=%v", changed, err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 0 {
		t.Fatalf("credits after invalid refund = %d, want 0", got)
	}
	row := loadReservation(t, pool, ids.userID, ids.opID)
	if row.status != string(domain.BillingReserved) {
		t.Fatalf("status = %s, want reserved", row.status)
	}
	if countLedgerByReason(t, pool, ids.userID, "refund") != 0 {
		t.Fatal("invalid reason wrote a refund ledger row")
	}
}

func TestReservationLedgerReferencesOperationNotTask(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 2)
	if _, _, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
	}); err != nil {
		t.Fatal(err)
	}
	entry := loadLedger(t, pool, ids.userID, "reserve")
	if entry.delta != -1 || entry.refType != "operation" || entry.refID != ids.opID {
		t.Fatalf("reserve ledger = %+v, want operation/%s delta=-1", entry, ids.opID)
	}
	if _, _, err := store.Refund(context.Background(), domain.RefundBilling{
		UserID: ids.userID, OperationID: ids.opID, Reason: "failed",
	}); err != nil {
		t.Fatal(err)
	}
	refund := loadLedger(t, pool, ids.userID, "refund")
	if refund.delta != 1 || refund.refType != "operation" || refund.refID != ids.opID {
		t.Fatalf("refund ledger = %+v", refund)
	}
}

func TestReservationConcurrentDeductsOnce(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 20)
	var created atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := store.Reserve(context.Background(), domain.ReserveBilling{
				UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
			})
			if err != nil {
				errs <- err
				return
			}
			if ok {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if created.Load() != 1 {
		t.Fatalf("created = %d, want 1", created.Load())
	}
	if got := walletCredits(t, pool, ids.userID); got != 19 {
		t.Fatalf("credits after concurrent reserve = %d, want 19", got)
	}
	if n := countLedgerByReason(t, pool, ids.userID, "reserve"); n != 1 {
		t.Fatalf("reserve ledger rows = %d, want 1", n)
	}
}

func TestRefundConcurrentCreditsOnce(t *testing.T) {
	store, pool, ids := newBillingRepo(t, 1)
	if _, _, err := store.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "assessment", Units: 1,
	}); err != nil {
		t.Fatal(err)
	}
	var changed atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := store.Refund(context.Background(), domain.RefundBilling{
				UserID: ids.userID, OperationID: ids.opID, Reason: "cancelled",
			})
			if err != nil {
				errs <- err
				return
			}
			if ok {
				changed.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if changed.Load() != 1 {
		t.Fatalf("refunded = %d, want 1", changed.Load())
	}
	if got := walletCredits(t, pool, ids.userID); got != 1 {
		t.Fatalf("credits after concurrent refund = %d, want 1", got)
	}
	if n := countLedgerByReason(t, pool, ids.userID, "refund"); n != 1 {
		t.Fatalf("refund ledger rows = %d, want 1", n)
	}
}

type billingIDs struct {
	userID   string
	opID     string
	resultID string
}

func newBillingRepo(t *testing.T, credits int) (*Store, *pgxpool.Pool, billingIDs) {
	t.Helper()
	pool := testutil.NewPostgres(t)
	store := New(pool)
	userID := insertUser(t, store)
	op := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationRender, Status: domain.OperationAccepted,
	})
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO billing_wallets(user_id, credits) VALUES($1::uuid, $2)`, userID, credits); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return store, pool, billingIDs{userID: userID, opID: op.ID, resultID: uuid.NewString()}
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

func countLedgerByReason(t *testing.T, pool *pgxpool.Pool, userID, reason string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM billing_ledger WHERE user_id=$1::uuid AND reason=$2`, userID, reason).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type ledgerRow struct {
	delta   int
	refType string
	refID   string
}

func loadLedger(t *testing.T, pool *pgxpool.Pool, userID, reason string) ledgerRow {
	t.Helper()
	var row ledgerRow
	err := pool.QueryRow(context.Background(), `
		SELECT delta, reference_type, reference_id::text
		FROM billing_ledger WHERE user_id=$1::uuid AND reason=$2`, userID, reason).Scan(&row.delta, &row.refType, &row.refID)
	if err != nil {
		t.Fatalf("load ledger %s: %v", reason, err)
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
