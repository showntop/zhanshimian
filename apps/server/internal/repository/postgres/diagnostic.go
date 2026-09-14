package postgres

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/diagnostic"
)

var _ diagnostic.Writer = (*Store)(nil)

const diagnosticSelectSQL = `
	SELECT d.id::text, d.kind, d.scene, d.conclusion, d.priority_title, d.priority_copy,
	       d.tags, d.findings, d.options, d.saved, d.created_at,
	       ma.id::text,
	       CASE ma.origin
	         WHEN 'user_upload' THEN 'user_original'
	         WHEN 'provider_output' THEN 'generated_preview'
	         WHEN 'bundled_reference' THEN 'bundled_reference'
	         WHEN 'demo' THEN 'demo_example'
	         ELSE ''
	       END,
	       CASE ma.display_kind
	         WHEN 'original' THEN '原本'
	         WHEN 'generated_reference' THEN '风格参考'
	         WHEN 'effect_example' THEN '效果示例'
	         WHEN 'style_reference' THEN '风格参考'
	         ELSE ''
	       END
	FROM diagnostics d
	LEFT JOIN media_assets ma ON ma.user_id=d.user_id AND ma.id=d.source_media_asset_id`

func (s *Store) InsertDiagnostic(ctx context.Context, userID string, d diagnostic.Diagnosis) (diagnostic.Diagnosis, error) {
	tags, _ := json.Marshal(d.Tags)
	findings, err := json.Marshal(d.Findings)
	if err != nil {
		return d, err
	}
	options, err := json.Marshal(d.Options)
	if err != nil {
		return d, err
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO diagnostics(user_id, kind, scene, conclusion, priority_title, priority_copy,
		                        tags, findings, options, source_media_asset_id)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10,'')::uuid)
		RETURNING id::text, created_at`,
		userID, d.Kind, d.Scene, d.Conclusion, d.PriorityTitle, d.PriorityCopy,
		tags, findings, options, d.MediaAssetID).
		Scan(&d.ID, &d.CreatedAt)
	return d, mapNotFound(err)
}

func (s *Store) GetDiagnosticByID(ctx context.Context, userID string, id string) (diagnostic.Diagnosis, error) {
	return s.scanDiagnostic(s.pool.QueryRow(ctx, diagnosticSelectSQL+` WHERE d.user_id=$1::uuid AND d.id=$2::uuid`, userID, id))
}

func (s *Store) GetLatestDiagnosticByKind(ctx context.Context, userID string, kind string) (diagnostic.Diagnosis, error) {
	return s.scanDiagnostic(s.pool.QueryRow(ctx, diagnosticSelectSQL+`
		WHERE d.user_id=$1::uuid AND d.kind=$2
		ORDER BY d.created_at DESC LIMIT 1`, userID, kind))
}

func (s *Store) UpdateDiagnosticSaved(ctx context.Context, userID string, id string, saved bool) (diagnostic.Diagnosis, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE diagnostics SET saved=$3 WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id, saved)
	if err != nil {
		return diagnostic.Diagnosis{}, err
	}
	if tag.RowsAffected() == 0 {
		return diagnostic.Diagnosis{}, repository.ErrNotFound
	}
	return s.GetDiagnosticByID(ctx, userID, id)
}

func (s *Store) scanDiagnostic(row rowScanner) (diagnostic.Diagnosis, error) {
	var d diagnostic.Diagnosis
	var tags, findings, optionsJSON []byte
	var mediaAssetID, sourceKind, displayLabel *string
	err := row.Scan(&d.ID, &d.Kind, &d.Scene, &d.Conclusion, &d.PriorityTitle, &d.PriorityCopy,
		&tags, &findings, &optionsJSON, &d.Saved, &d.CreatedAt,
		&mediaAssetID, &sourceKind, &displayLabel)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal(tags, &d.Tags)
	_ = json.Unmarshal(findings, &d.Findings)
	_ = json.Unmarshal(optionsJSON, &d.Options)
	if mediaAssetID != nil && *mediaAssetID != "" {
		d.SourceMedia = &domain.RenderMediaView{
			AssetID: *mediaAssetID, SourceKind: deref(sourceKind), DisplayLabel: deref(displayLabel),
		}
	}
	return d, nil
}
