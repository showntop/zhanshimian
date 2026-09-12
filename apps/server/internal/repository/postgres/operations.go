package postgres

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

const operationSelect = `
	SELECT id::text, user_id::text, kind, subject_type, subject_id::text,
	       status, progress_bps, stage_code, public_message, error_code, trace_id,
	       retryable, result_type, result_id::text, version, created_at, updated_at, finished_at
	FROM operations`

func (s *Store) GetOperation(ctx context.Context, userID, id string) (domain.Operation, error) {
	op, err := scanOperation(s.pool.QueryRow(ctx, operationSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id))
	return op, mapNotFound(err)
}

func (s *Store) GetOperations(ctx context.Context, userID string, ids []string) ([]domain.Operation, error) {
	if len(ids) == 0 {
		return nil, repository.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, operationSelect+` WHERE user_id=$1::uuid AND id=ANY($2::uuid[])`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]domain.Operation, len(ids))
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		byID[op.ID] = op
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(byID) != len(ids) {
		return nil, repository.ErrNotFound
	}
	out := make([]domain.Operation, 0, len(ids))
	for _, id := range ids {
		op, ok := byID[id]
		if !ok {
			return nil, repository.ErrNotFound
		}
		out = append(out, op)
	}
	return out, nil
}

func scanOperation(row rowScanner) (domain.Operation, error) {
	var op domain.Operation
	var errorCode, traceID, resultType, resultID *string
	var finishedAt *time.Time
	err := row.Scan(
		&op.ID, &op.UserID, &op.Kind, &op.SubjectType, &op.SubjectID,
		&op.Status, &op.ProgressBPS, &op.StageCode, &op.PublicMessage, &errorCode, &traceID,
		&op.Retryable, &resultType, &resultID, &op.Version, &op.CreatedAt, &op.UpdatedAt, &finishedAt,
	)
	if errorCode != nil {
		op.ErrorCode = *errorCode
	}
	if traceID != nil {
		op.TraceID = *traceID
	}
	if resultType != nil {
		op.ResultType = *resultType
	}
	if resultID != nil {
		op.ResultID = *resultID
	}
	op.FinishedAt = finishedAt
	return op, err
}
