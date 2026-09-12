package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
)

const idempotencySelect = `
	SELECT id::text, user_id::text, key, request_fingerprint, status,
	       response_status, response_body, scope, resource_id::text, expires_at, created_at
	FROM idempotency_keys`

func (s *Store) BeginIdempotency(ctx context.Context, in domain.BeginIdempotency) (domain.IdempotencyRecord, domain.IdempotencyBeginOutcome, error) {
	rec, err := insertIdempotency(ctx, s, in)
	if err == nil {
		return rec, domain.IdempotencyBeginStarted, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.IdempotencyRecord{}, "", err
	}
	existing, err := s.getIdempotency(ctx, in.UserID, in.Key)
	if err != nil {
		return domain.IdempotencyRecord{}, "", err
	}
	if !existing.ExpiresAt.After(time.Now()) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE user_id=$1::uuid AND key=$2 AND expires_at<=now()`, in.UserID, in.Key)
		rec, err = insertIdempotency(ctx, s, in)
		if err == nil {
			return rec, domain.IdempotencyBeginStarted, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.IdempotencyRecord{}, "", err
		}
		existing, err = s.getIdempotency(ctx, in.UserID, in.Key)
		if err != nil {
			return domain.IdempotencyRecord{}, "", err
		}
	}
	if existing.RequestFingerprint != in.RequestFingerprint {
		return existing, domain.IdempotencyBeginConflict, nil
	}
	if existing.Status == domain.IdempotencyCompleted {
		return existing, domain.IdempotencyBeginReplay, nil
	}
	return existing, domain.IdempotencyBeginInProgress, nil
}

func (s *Store) CompleteIdempotency(ctx context.Context, userID, key string, status int, body json.RawMessage) error {
	if !json.Valid(body) {
		if len(body) == 0 {
			body = json.RawMessage("null")
		} else {
			encoded, err := json.Marshal(string(body))
			if err != nil {
				return err
			}
			body = encoded
		}
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE idempotency_keys
		SET status='completed', response_status=$3, response_body=$4::jsonb
		WHERE user_id=$1::uuid AND key=$2 AND status='in_progress'`,
		userID, key, status, []byte(body))
	return err
}

func (s *Store) AbortIdempotency(ctx context.Context, userID, key string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM idempotency_keys
		WHERE user_id=$1::uuid AND key=$2 AND status='in_progress'`,
		userID, key)
	return err
}

func insertIdempotency(ctx context.Context, s *Store, in domain.BeginIdempotency) (domain.IdempotencyRecord, error) {
	return scanIdempotency(s.pool.QueryRow(ctx, `
		INSERT INTO idempotency_keys (user_id, key, request_fingerprint, status, scope, expires_at)
		VALUES ($1::uuid, $2, $3, 'in_progress', $4, $5)
		ON CONFLICT (user_id, key) DO NOTHING
		`+idempotencyReturning, in.UserID, in.Key, in.RequestFingerprint, nullIfEmpty(in.Scope), in.ExpiresAt))
}

func (s *Store) getIdempotency(ctx context.Context, userID, key string) (domain.IdempotencyRecord, error) {
	return scanIdempotency(s.pool.QueryRow(ctx, idempotencySelect+` WHERE user_id=$1::uuid AND key=$2`, userID, key))
}

const idempotencyReturning = `
		RETURNING id::text, user_id::text, key, request_fingerprint, status,
		          response_status, response_body, scope, resource_id::text, expires_at, created_at`

func scanIdempotency(row rowScanner) (domain.IdempotencyRecord, error) {
	var rec domain.IdempotencyRecord
	var responseStatus *int
	var responseBody []byte
	var scope, resourceID *string
	err := row.Scan(
		&rec.ID, &rec.UserID, &rec.Key, &rec.RequestFingerprint, &rec.Status,
		&responseStatus, &responseBody, &scope, &resourceID, &rec.ExpiresAt, &rec.CreatedAt,
	)
	if responseStatus != nil {
		rec.ResponseStatus = *responseStatus
	}
	if len(responseBody) > 0 {
		rec.ResponseBody = responseBody
	}
	if scope != nil {
		rec.Scope = *scope
	}
	if resourceID != nil {
		rec.ResourceID = *resourceID
	}
	return rec, err
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
