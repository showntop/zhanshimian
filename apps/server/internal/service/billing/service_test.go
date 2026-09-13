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

func TestReserveIsIdempotent(t *testing.T) {
	repo, pool, ids := newBillingRepo(t, 3)
	service := New(repo)
	first, err := service.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1)
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	second, err := service.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1)
	if err != nil {
		t.Fatalf("second Reserve: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("reservation IDs differ: %s vs %s", first.ID, second.ID)
	}
	if got := walletCredits(t, pool, ids.userID); got != 2 {
		t.Fatalf("credits after idempotent reserve = %d, want 2", got)
	}
}

func TestRefundAfterSettleErrors(t *testing.T) {
	repo, pool, ids := newBillingRepo(t, 3)
	service := New(repo)
	if _, err := service.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductAssessment, 1); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	markOperationSucceeded(t, pool, ids.userID, ids.opID, "report", uuid.NewString())
	if err := service.Settle(context.Background(), ids.userID, ids.opID, nil); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if err := service.Refund(context.Background(), ids.userID, ids.opID); !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("Refund after settle error = %v, want ErrAlreadySettled", err)
	}
}

func TestRefundReturnsCreditsExactlyOnce(t *testing.T) {
	repo, pool, ids := newBillingRepo(t, 1)
	service := New(repo)
	if _, err := service.Reserve(context.Background(), ids.userID, ids.opID, domain.ProductRenderPublication, 1); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	markOperationFailed(t, pool, ids.userID, ids.opID)
	if err := service.Refund(context.Background(), ids.userID, ids.opID); err != nil {
		t.Fatalf("first Refund: %v", err)
	}
	if err := service.Refund(context.Background(), ids.userID, ids.opID); err != nil {
		t.Fatalf("second Refund: %v", err)
	}
	if got := walletCredits(t, pool, ids.userID); got != 1 {
		t.Fatalf("credits after refund = %d, want 1", got)
	}
}

type billingIDs struct {
	userID string
	opID   string
}

func newBillingRepo(t *testing.T, credits int) (Lifecycle, *pgxpool.Pool, billingIDs) {
	t.Helper()
	pool := testutil.NewPostgres(t)
	store := postgres.New(pool)
	userID := insertUser(t, pool)
	opID := insertOperation(t, pool, userID)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO billing_wallets(user_id, credits) VALUES($1::uuid, $2)`, userID, credits); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return store, pool, billingIDs{userID: userID, opID: opID}
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

func markOperationSucceeded(t *testing.T, pool *pgxpool.Pool, userID, opID, resultType, resultID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE operations SET status='succeeded', result_type=$3, result_id=$4::uuid, finished_at=now(), updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, opID, resultType, resultID); err != nil {
		t.Fatalf("mark operation succeeded: %v", err)
	}
}

func markOperationFailed(t *testing.T, pool *pgxpool.Pool, userID, opID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE operations SET status='failed', trace_id='svc-test-fail', finished_at=now(), updated_at=now()
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
