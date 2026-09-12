package postgres

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type invocationLedger interface {
	StartInvocation(context.Context, domain.StartInvocation) (domain.ProviderInvocation, error)
	FinishInvocation(context.Context, domain.FinishInvocation) error
}

var _ invocationLedger = (*Store)(nil)

const invocationReturning = `
	id::text, user_id::text, operation_id::text, task_id::text, attempt_no,
	capability, routing_config_version, provider_key, model_key, protocol,
	request_hash, provider_request_id, status,
	input_tokens, output_tokens, input_images, output_images,
	estimated_cost_cny, latency_ms, error_class, error_code,
	started_at, finished_at, created_at`

func (s *Store) StartInvocation(ctx context.Context, in domain.StartInvocation) (domain.ProviderInvocation, error) {
	return scanInvocation(s.pool.QueryRow(ctx, `
		INSERT INTO provider_invocations (
			user_id, operation_id, task_id, attempt_no, capability,
			routing_config_version, provider_key, model_key, protocol,
			request_hash, status, input_images
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5,
			$6, $7, $8, $9,
			$10, 'started', $11
		) RETURNING`+invocationReturning,
		in.UserID, in.OperationID, in.TaskID, in.AttemptNo, in.Capability,
		in.RoutingConfigVersion, in.ProviderKey, in.ModelKey, in.Protocol,
		in.RequestHash, in.InputImages))
}

func (s *Store) FinishInvocation(ctx context.Context, in domain.FinishInvocation) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE provider_invocations
		SET
			status = $5,
			provider_request_id = NULLIF($6, ''),
			input_tokens = $7,
			output_tokens = $8,
			input_images = COALESCE($9, input_images),
			output_images = $10,
			estimated_cost_cny = $11,
			latency_ms = $12,
			error_class = NULLIF($13, ''),
			error_code = NULLIF($14, ''),
			finished_at = now()
		WHERE user_id = $1::uuid
		  AND id = $2::uuid
		  AND task_id = $3::uuid
		  AND attempt_no = $4
		  AND status = 'started'`,
		in.UserID, in.InvocationID, in.TaskID, in.AttemptNo,
		string(in.Status), in.ProviderRequestID,
		in.InputTokens, in.OutputTokens, in.InputImages, in.OutputImages,
		in.EstimatedCostCNY, in.LatencyMS,
		string(in.ErrorClass), in.ErrorCode,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrConflict
	}
	return nil
}

func scanInvocation(row rowScanner) (domain.ProviderInvocation, error) {
	var inv domain.ProviderInvocation
	var providerRequestID, errorClass, errorCode *string
	err := row.Scan(
		&inv.ID, &inv.UserID, &inv.OperationID, &inv.TaskID, &inv.AttemptNo,
		&inv.Capability, &inv.RoutingConfigVersion, &inv.ProviderKey, &inv.ModelKey, &inv.Protocol,
		&inv.RequestHash, &providerRequestID, &inv.Status,
		&inv.InputTokens, &inv.OutputTokens, &inv.InputImages, &inv.OutputImages,
		&inv.EstimatedCostCNY, &inv.LatencyMS, &errorClass, &errorCode,
		&inv.StartedAt, &inv.FinishedAt, &inv.CreatedAt,
	)
	if providerRequestID != nil {
		inv.ProviderRequestID = *providerRequestID
	}
	if errorClass != nil {
		inv.ErrorClass = domain.ErrorClass(*errorClass)
	}
	if errorCode != nil {
		inv.ErrorCode = *errorCode
	}
	return inv, err
}
