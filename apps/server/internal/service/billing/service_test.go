package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestReservationLifecycleIsIdempotent(t *testing.T) {
	repo, pool, ids := newBillingRepo(t, 3)
	service := New(repo)
	first, created, err := service.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
	})
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if !created {
		t.Fatal("first Reserve created=false, want true")
	}
	second, created, err := service.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "render", Units: 1,
	})
	if err != nil {
		t.Fatalf("second Reserve: %v", err)
	}
	if created {
		t.Fatal("second Reserve created=true, want false")
	}
	if first.ID != second.ID {
		t.Fatalf("reservation IDs differ: %s vs %s", first.ID, second.ID)
	}
	if got := walletCredits(t, pool, ids.userID); got != 2 {
		t.Fatalf("credits after idempotent reserve = %d, want 2", got)
	}
	_, settled, err := service.Settle(context.Background(), domain.SettleBilling{
		UserID: ids.userID, OperationID: ids.opID,
		ResultType: "render_publication", ResultID: ids.resultID,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if !settled {
		t.Fatal("Settle settled=false, want true")
	}
	_, refunded, err := service.Refund(context.Background(), domain.RefundBilling{
		UserID: ids.userID, OperationID: ids.opID, Reason: "failed",
	})
	if !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("Refund after settle error = %v, want ErrAlreadySettled", err)
	}
	if refunded {
		t.Fatal("Refund after settle refunded=true, want false")
	}
}

func TestRefundReturnsCreditsExactlyOnce(t *testing.T) {
	repo, pool, ids := newBillingRepo(t, 1)
	service := New(repo)
	if _, _, err := service.Reserve(context.Background(), domain.ReserveBilling{
		UserID: ids.userID, OperationID: ids.opID, Kind: "assessment", Units: 1,
	}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	_, changed, err := service.Refund(context.Background(), domain.RefundBilling{
		UserID: ids.userID, OperationID: ids.opID, Reason: "cancelled",
	})
	if err != nil {
		t.Fatalf("first Refund: %v", err)
	}
	if !changed {
		t.Fatal("first Refund changed=false, want true")
	}
	_, changed, err = service.Refund(context.Background(), domain.RefundBilling{
		UserID: ids.userID, OperationID: ids.opID, Reason: "cancelled",
	})
	if err != nil {
		t.Fatalf("second Refund: %v", err)
	}
	if changed {
		t.Fatal("second Refund changed=true, want false")
	}
	if got := walletCredits(t, pool, ids.userID); got != 1 {
		t.Fatalf("credits after refund = %d, want 1", got)
	}
}

type billingIDs struct {
	userID   string
	opID     string
	resultID string
}

func newBillingRepo(t *testing.T, credits int) (Repository, *pgxpool.Pool, billingIDs) {
	t.Helper()
	pool := testutil.NewPostgres(t)
	store := postgres.New(pool)
	userID := insertUser(t, pool)
	opID := insertOperation(t, pool, userID)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO billing_wallets(user_id, credits) VALUES($1::uuid, $2)`, userID, credits); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return store, pool, billingIDs{userID: userID, opID: opID, resultID: uuid.NewString()}
}

func insertUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var userID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO users(nickname) VALUES('billing-svc') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func insertOperation(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var opID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO operations(user_id, kind, subject_type, subject_id, status)
		VALUES ($1::uuid, 'assessment', 'assessment', $2::uuid, 'accepted')
		RETURNING id::text`, userID, uuid.NewString()).Scan(&opID)
	if err != nil {
		t.Fatalf("seed operation: %v", err)
	}
	return opID
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
