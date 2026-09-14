package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/share"
)

var _ share.Writer = (*Store)(nil)

func (s *Store) InsertShare(ctx context.Context, userID string, source share.Source, snapshot share.Snapshot, includePhoto bool) (share.Card, error) {
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return share.Card{}, err
	}
	card := share.Card{
		SourceType: source.SourceType, SourceID: source.SourceID,
		Snapshot: snapshot, IncludePhoto: includePhoto,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).UTC(),
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO shares(user_id, token, source_type, source_id, asset_id, snapshot, include_photo, expires_at)
		VALUES ($1::uuid, gen_random_uuid()::text, $2, $3::uuid, NULLIF($4,'')::uuid, $5, $6, $7)
		RETURNING id::text, token, created_at`,
		userID, source.SourceType, source.SourceID, source.AssetID, snapshotJSON, includePhoto, card.ExpiresAt).
		Scan(&card.ID, &card.Token, &card.CreatedAt)
	return card, mapNotFound(err)
}

func (s *Store) GetPublicShareByToken(ctx context.Context, token string) (share.Card, error) {
	return s.scanShare(s.pool.QueryRow(ctx, shareSelect+`
		WHERE s.token=$1 AND s.revoked=false AND s.expires_at > now()`, token))
}

func (s *Store) RevokeShareByID(ctx context.Context, userID string, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE shares SET revoked=true WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

const shareSelect = `
	SELECT s.id::text, s.token, s.source_type, s.source_id::text, s.snapshot,
	       s.include_photo, s.revoked, s.expires_at, s.created_at,
	       COALESCE(ma.object_key, '')
	FROM shares s
	LEFT JOIN media_assets ma ON ma.user_id = s.user_id AND ma.id = s.asset_id`

func (s *Store) scanShare(row rowScanner) (share.Card, error) {
	var card share.Card
	var snapshotJSON []byte
	err := row.Scan(&card.ID, &card.Token, &card.SourceType, &card.SourceID, &snapshotJSON,
		&card.IncludePhoto, &card.Revoked, &card.ExpiresAt, &card.CreatedAt, &card.ObjectKey)
	if err != nil {
		return card, mapNotFound(err)
	}
	if err := json.Unmarshal(snapshotJSON, &card.Snapshot); err != nil {
		return card, err
	}
	return card, nil
}
