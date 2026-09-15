package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
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

// operationKindNames 把领域枚举转成 SQL text[] 参数。
func operationKindNames(kinds []domain.OperationKind) []string {
	names := make([]string, len(kinds))
	for i, kind := range kinds {
		names[i] = string(kind)
	}
	return names
}

// CountOperationsCreatedSince 计数某用户在 since 之后创建的 operation 行
// （用量日限的仓储自计数；subjectTypes 为空不过滤）。日界由调用方按服务器
// 本地自然日算好传入。
func (s *Store) CountOperationsCreatedSince(ctx context.Context, userID string, kinds []domain.OperationKind, subjectTypes []string, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM operations
		WHERE user_id=$1::uuid AND kind=ANY($2::text[])
		  AND ($3::text[] IS NULL OR subject_type=ANY($3::text[]))
		  AND created_at>=$4`,
		userID, operationKindNames(kinds), subjectTypes, since).Scan(&count)
	return count, err
}

// CountActiveOperations 计数某用户非终态的 operation 行（在途并发闸）。
func (s *Store) CountActiveOperations(ctx context.Context, userID string, subjectTypes []string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM operations
		WHERE user_id=$1::uuid AND status IN ('accepted','running','retrying')
		  AND subject_type=ANY($2::text[])`,
		userID, subjectTypes).Scan(&count)
	return count, err
}

// FailStaleOperation 把超过在途阈值的孤儿 operation 落失败终态（读路径兜底，
// 对应旧线 GetAnalysis 的 stale 判定）：仅当行仍非终态、且 operation 与其全部
// task 的最近活动都早于阈值时才写入。单语句 CAS——worker 若并发刷新
// updated_at，READ COMMITTED 下本语句会在最新行版本上重估 WHERE 并自动放弃，
// 不会误杀活着的任务。失败写入带 public_message/retryable=true（S1b 模式）。
func (s *Store) FailStaleOperation(ctx context.Context, userID, id string, idleFor time.Duration, code, publicMessage string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE operations o
		SET status='failed', error_code=$4, public_message=$5, retryable=true,
		    trace_id=$6, version=o.version+1, updated_at=now(), finished_at=now()
		WHERE o.user_id=$1::uuid AND o.id=$2::uuid
		  AND o.status IN ('accepted','running','retrying')
		  AND GREATEST(o.updated_at, COALESCE((
		        SELECT max(t.updated_at) FROM tasks t
		        WHERE t.user_id=o.user_id AND t.operation_id=o.id
		      ), o.updated_at)) < now() - $3::interval`,
		userID, id, pgInterval(idleFor), code, publicMessage, uuid.NewString())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
