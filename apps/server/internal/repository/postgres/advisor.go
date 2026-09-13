package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/advisor"
)

var _ advisor.Writer = (*Store)(nil)

func (s *Store) AppendExchange(ctx context.Context, userID string, question string, reply string, actions []domain.AdvisorAction) (domain.AdvisorMessage, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var conversationID string
	err = tx.QueryRow(ctx, `
		INSERT INTO advisor_conversations(user_id)
		VALUES ($1::uuid)
		RETURNING id::text`, userID).Scan(&conversationID)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}

	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	message := domain.AdvisorMessage{
		ConversationID: conversationID, Role: "assistant", Content: reply, Actions: actions,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO advisor_messages(user_id, conversation_id, role, content, actions)
		VALUES ($1::uuid, $2::uuid, 'user', $3, '[]'::jsonb)
		RETURNING id::text, created_at`,
		userID, conversationID, question).Scan(&message.ID, &message.CreatedAt)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO advisor_messages(user_id, conversation_id, role, content, actions)
		VALUES ($1::uuid, $2::uuid, 'assistant', $3, $4)
		RETURNING id::text, created_at`,
		userID, conversationID, reply, actionsJSON).Scan(&message.ID, &message.CreatedAt)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AdvisorMessage{}, err
	}
	return message, nil
}

func (s *Store) ListMessages(ctx context.Context, userID string) ([]domain.AdvisorMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id::text, m.conversation_id::text, m.role, m.content, m.actions, m.created_at
		FROM advisor_messages m
		JOIN advisor_conversations c ON c.user_id=m.user_id AND c.id=m.conversation_id
		WHERE m.user_id=$1::uuid
		ORDER BY m.created_at, m.id`, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return []domain.AdvisorMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AdvisorMessage{}
	for rows.Next() {
		var message domain.AdvisorMessage
		var actions []byte
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &actions, &message.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(actions, &message.Actions); err != nil {
			return nil, err
		}
		out = append(out, message)
	}
	return out, rows.Err()
}

func (s *Store) ApplyAction(ctx context.Context, userID string, actionID string) (domain.AdvisorAction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id::text, m.conversation_id::text, m.role, m.content, m.actions, m.created_at
		FROM advisor_messages m
		WHERE m.user_id=$1::uuid`, userID)
	if err != nil {
		return domain.AdvisorAction{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var message domain.AdvisorMessage
		var actions []byte
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &actions, &message.CreatedAt); err != nil {
			return domain.AdvisorAction{}, err
		}
		if err := json.Unmarshal(actions, &message.Actions); err != nil {
			return domain.AdvisorAction{}, err
		}
		for i, action := range message.Actions {
			if action.ID != actionID {
				continue
			}
			action.Applied = true
			message.Actions[i] = action
			updated, err := json.Marshal(message.Actions)
			if err != nil {
				return domain.AdvisorAction{}, err
			}
			if _, err := s.pool.Exec(ctx,
				`UPDATE advisor_messages SET actions=$3 WHERE user_id=$1::uuid AND id=$2::uuid`,
				userID, message.ID, updated); err != nil {
				return domain.AdvisorAction{}, err
			}
			return action, nil
		}
	}
	if err := rows.Err(); err != nil {
		return domain.AdvisorAction{}, err
	}
	return domain.AdvisorAction{}, repository.ErrNotFound
}
