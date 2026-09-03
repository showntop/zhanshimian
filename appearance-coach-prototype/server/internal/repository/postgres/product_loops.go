package postgres

import (
	"context"
	"encoding/json"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/repository"
)

func scanTodayPlan(row interface{ Scan(...any) error }) (domain.TodayPlan, error) {
	var item domain.TodayPlan
	var contextData, stepsData []byte
	err := row.Scan(&item.ID, &item.ReportID, &contextData, &item.Title, &item.Summary, &item.ImageURL, &stepsData, &item.Active, &item.Feedback, &item.RegenerateCount, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(contextData, &item.Context)
	}
	if err == nil {
		err = json.Unmarshal(stepsData, &item.Steps)
	}
	return item, err
}

const todayPlanSelect = `SELECT id::text,coalesce(report_id::text,''),context,title,summary,image_url,steps,active,feedback,regenerate_count,created_at,updated_at FROM today_plans`

func (s *Store) GetTodayPlan(ctx context.Context, userID string) (domain.TodayPlan, error) {
	item, err := scanTodayPlan(s.pool.QueryRow(ctx, todayPlanSelect+` WHERE user_id=$1 AND plan_date=current_date`, userID))
	return item, mapNotFound(err)
}

func (s *Store) SaveTodayPlan(ctx context.Context, userID string, input domain.TodayPlan) (domain.TodayPlan, error) {
	contextData, err := json.Marshal(input.Context)
	if err != nil {
		return domain.TodayPlan{}, err
	}
	stepsData, err := json.Marshal(input.Steps)
	if err != nil {
		return domain.TodayPlan{}, err
	}
	var reportID any
	if input.ReportID != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reports WHERE id=$1 AND user_id=$2)`, input.ReportID, userID).Scan(&exists); err != nil {
			return domain.TodayPlan{}, err
		}
		if !exists {
			return domain.TodayPlan{}, repository.ErrNotFound
		}
		reportID = input.ReportID
	}
	item, err := scanTodayPlan(s.pool.QueryRow(ctx, `
		INSERT INTO today_plans(user_id,report_id,plan_date,context,title,summary,image_url,steps,regenerate_count)
		VALUES($1,$2,$3::date,$4,$5,$6,$7,$8,$9)
		ON CONFLICT(user_id,plan_date) DO UPDATE SET
			report_id=EXCLUDED.report_id,context=EXCLUDED.context,title=EXCLUDED.title,summary=EXCLUDED.summary,
			image_url=EXCLUDED.image_url,steps=EXCLUDED.steps,regenerate_count=EXCLUDED.regenerate_count,updated_at=now(),
			active=false,feedback=''
		RETURNING id::text,coalesce(report_id::text,''),context,title,summary,image_url,steps,active,feedback,regenerate_count,created_at,updated_at`,
		userID, reportID, input.Context.Date, contextData, input.Title, input.Summary, input.ImageURL, stepsData, input.RegenerateCount))
	return item, err
}

func (s *Store) ActivateTodayPlan(ctx context.Context, userID, planID string) (domain.TodayPlan, error) {
	item, err := scanTodayPlan(s.pool.QueryRow(ctx, todayPlanSelect+` WHERE id=$1 AND user_id=$2`, planID, userID))
	if err != nil {
		return item, mapNotFound(err)
	}
	item.Active = true
	_, err = s.pool.Exec(ctx, `UPDATE today_plans SET active=true,updated_at=now() WHERE id=$1 AND user_id=$2`, planID, userID)
	return item, err
}

func (s *Store) FeedbackTodayPlan(ctx context.Context, userID, planID, feedback string) (domain.TodayPlan, error) {
	item, err := scanTodayPlan(s.pool.QueryRow(ctx, `
		UPDATE today_plans SET feedback=$3,updated_at=now() WHERE id=$1 AND user_id=$2
		RETURNING id::text,coalesce(report_id::text,''),context,title,summary,image_url,steps,active,feedback,regenerate_count,created_at,updated_at`, planID, userID, feedback))
	return item, mapNotFound(err)
}

func (s *Store) CreateShareCard(ctx context.Context, userID string, input domain.ShareCardInput, snapshot json.RawMessage) (domain.ShareCard, error) {
	var item domain.ShareCard
	err := s.pool.QueryRow(ctx, `
		INSERT INTO share_cards(user_id,source_type,source_id,snapshot,include_photo)
		VALUES($1,$2,$3,$4,$5)
		RETURNING id::text,token,source_type,source_id::text,snapshot,include_photo,false,expires_at,created_at`,
		userID, input.SourceType, input.SourceID, snapshot, input.IncludePhoto).
		Scan(&item.ID, &item.Token, &item.SourceType, &item.SourceID, &item.Snapshot, &item.IncludePhoto, &item.Revoked, &item.ExpiresAt, &item.CreatedAt)
	return item, err
}

func (s *Store) GetShareCard(ctx context.Context, token string) (domain.ShareCard, error) {
	var item domain.ShareCard
	err := s.pool.QueryRow(ctx, `
		SELECT id::text,token,source_type,source_id::text,snapshot,include_photo,(revoked_at IS NOT NULL),expires_at,created_at
		FROM share_cards WHERE token=$1 AND revoked_at IS NULL AND expires_at>now()`, token).
		Scan(&item.ID, &item.Token, &item.SourceType, &item.SourceID, &item.Snapshot, &item.IncludePhoto, &item.Revoked, &item.ExpiresAt, &item.CreatedAt)
	return item, mapNotFound(err)
}

func (s *Store) RevokeShareCard(ctx context.Context, userID, cardID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE share_cards SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, cardID, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return err
}

const wardrobeSelect = `SELECT id::text,coalesce(media_id::text,''),name,category,color,season,formality,scenes,image_url,favorite,wear_count,created_at,updated_at FROM wardrobe_items`

func scanWardrobeItem(row interface{ Scan(...any) error }) (domain.WardrobeItem, error) {
	var item domain.WardrobeItem
	err := row.Scan(&item.ID, &item.MediaID, &item.Name, &item.Category, &item.Color, &item.Season, &item.Formality, &item.Scenes, &item.ImageURL, &item.Favorite, &item.WearCount, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) CreateWardrobeItem(ctx context.Context, userID string, input domain.WardrobeItemInput, imageURL string) (domain.WardrobeItem, error) {
	var mediaID any
	if input.MediaID != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets WHERE id=$1 AND user_id=$2 AND kind='wardrobe' AND deleted_at IS NULL)`, input.MediaID, userID).Scan(&exists); err != nil {
			return domain.WardrobeItem{}, err
		}
		if !exists {
			return domain.WardrobeItem{}, repository.ErrNotFound
		}
		mediaID = input.MediaID
	}
	item, err := scanWardrobeItem(s.pool.QueryRow(ctx, `
		INSERT INTO wardrobe_items(user_id,media_id,name,category,color,season,formality,scenes,image_url)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id::text,coalesce(media_id::text,''),name,category,color,season,formality,scenes,image_url,favorite,wear_count,created_at,updated_at`,
		userID, mediaID, input.Name, input.Category, input.Color, input.Season, input.Formality, input.Scenes, imageURL))
	return item, err
}

func (s *Store) ListWardrobeItems(ctx context.Context, userID string) ([]domain.WardrobeItem, error) {
	rows, err := s.pool.Query(ctx, wardrobeSelect+` WHERE user_id=$1 ORDER BY favorite DESC,updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.WardrobeItem, 0)
	for rows.Next() {
		item, err := scanWardrobeItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteWardrobeItem(ctx context.Context, userID, itemID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM wardrobe_items WHERE id=$1 AND user_id=$2`, itemID, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return err
}

func (s *Store) CreateWardrobeOutfit(ctx context.Context, userID string, items []domain.WardrobeItem, contextData json.RawMessage) (domain.WardrobeOutfit, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	var outfit domain.WardrobeOutfit
	outfit.Items = items
	outfit.Title = "现有衣橱 · 今日组合"
	outfit.Note = "优先复用你常穿的单品，用颜色与比例完成今天的表达。"
	err := s.pool.QueryRow(ctx, `
		INSERT INTO wardrobe_outfits(user_id,title,note,context,item_ids) VALUES($1,$2,$3,$4,$5)
		RETURNING id::text,title,note,context,item_ids,(worn_at IS NOT NULL),created_at`,
		userID, outfit.Title, outfit.Note, contextData, ids).
		Scan(&outfit.ID, &outfit.Title, &outfit.Note, &outfit.Context, &outfit.ItemIDs, &outfit.Worn, &outfit.CreatedAt)
	return outfit, err
}

func (s *Store) MarkWardrobeOutfitWorn(ctx context.Context, userID, outfitID string) (domain.WardrobeOutfit, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.WardrobeOutfit{}, err
	}
	defer tx.Rollback(ctx)
	var outfit domain.WardrobeOutfit
	err = tx.QueryRow(ctx, `
		UPDATE wardrobe_outfits SET worn_at=now() WHERE id=$1 AND user_id=$2
		RETURNING id::text,title,note,context,item_ids,true,created_at`, outfitID, userID).
		Scan(&outfit.ID, &outfit.Title, &outfit.Note, &outfit.Context, &outfit.ItemIDs, &outfit.Worn, &outfit.CreatedAt)
	if err != nil {
		return outfit, mapNotFound(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE wardrobe_items SET wear_count=wear_count+1,updated_at=now() WHERE user_id=$1 AND id=ANY($2::uuid[])`, userID, outfit.ItemIDs); err != nil {
		return outfit, err
	}
	rows, err := tx.Query(ctx, wardrobeSelect+` WHERE user_id=$1 AND id=ANY($2::uuid[]) ORDER BY array_position($2::uuid[],id)`, userID, outfit.ItemIDs)
	if err != nil {
		return outfit, err
	}
	items := make([]domain.WardrobeItem, 0, len(outfit.ItemIDs))
	for rows.Next() {
		item, err := scanWardrobeItem(rows)
		if err != nil {
			rows.Close()
			return outfit, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return outfit, err
	}
	rows.Close()
	outfit.Items = items
	return outfit, tx.Commit(ctx)
}

func (s *Store) CreateAdvisorConversation(ctx context.Context, userID string, contextData json.RawMessage) (domain.AdvisorConversation, error) {
	var item domain.AdvisorConversation
	err := s.pool.QueryRow(ctx, `
		INSERT INTO advisor_conversations(user_id,context) VALUES($1,$2)
		RETURNING id::text,title,context,created_at,updated_at`, userID, contextData).
		Scan(&item.ID, &item.Title, &item.Context, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) AddAdvisorExchange(ctx context.Context, userID, conversationID, userContent, assistantContent string, actions []domain.AdvisorAction) (domain.AdvisorMessage, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM advisor_conversations WHERE id=$1 AND user_id=$2)`, conversationID, userID).Scan(&exists); err != nil || !exists {
		if err != nil {
			return domain.AdvisorMessage{}, err
		}
		return domain.AdvisorMessage{}, repository.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `INSERT INTO advisor_messages(conversation_id,user_id,role,content) VALUES($1,$2,'user',$3)`, conversationID, userID, userContent); err != nil {
		return domain.AdvisorMessage{}, err
	}
	var item domain.AdvisorMessage
	err = tx.QueryRow(ctx, `
		INSERT INTO advisor_messages(conversation_id,user_id,role,content) VALUES($1,$2,'assistant',$3)
		RETURNING id::text,conversation_id::text,role,content,created_at`, conversationID, userID, assistantContent).
		Scan(&item.ID, &item.ConversationID, &item.Role, &item.Content, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	for _, action := range actions {
		if len(action.Payload) == 0 {
			action.Payload = json.RawMessage(`{}`)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO advisor_actions(message_id,user_id,kind,label,payload) VALUES($1,$2,$3,$4,$5)
			RETURNING id::text,kind,label,payload,applied`, item.ID, userID, action.Kind, action.Label, action.Payload).
			Scan(&action.ID, &action.Kind, &action.Label, &action.Payload, &action.Applied); err != nil {
			return item, err
		}
		item.Actions = append(item.Actions, action)
	}
	_, err = tx.Exec(ctx, `UPDATE advisor_conversations SET updated_at=now() WHERE id=$1`, conversationID)
	if err != nil {
		return item, err
	}
	return item, tx.Commit(ctx)
}

func (s *Store) ListAdvisorMessages(ctx context.Context, userID, conversationID string) ([]domain.AdvisorMessage, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM advisor_conversations WHERE id=$1 AND user_id=$2)`, conversationID, userID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, repository.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.id::text,m.conversation_id::text,m.role,m.content,m.created_at
		FROM advisor_messages m JOIN advisor_conversations c ON c.id=m.conversation_id
		WHERE m.conversation_id=$1 AND c.user_id=$2 ORDER BY m.created_at`, conversationID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.AdvisorMessage, 0)
	for rows.Next() {
		var item domain.AdvisorMessage
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Role, &item.Content, &item.CreatedAt); err != nil {
			return nil, err
		}
		actionRows, err := s.pool.Query(ctx, `SELECT id::text,kind,label,payload,applied FROM advisor_actions WHERE message_id=$1 ORDER BY created_at`, item.ID)
		if err != nil {
			return nil, err
		}
		for actionRows.Next() {
			var action domain.AdvisorAction
			if err := actionRows.Scan(&action.ID, &action.Kind, &action.Label, &action.Payload, &action.Applied); err != nil {
				actionRows.Close()
				return nil, err
			}
			item.Actions = append(item.Actions, action)
		}
		actionRows.Close()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ApplyAdvisorAction(ctx context.Context, userID, actionID string) (domain.AdvisorAction, error) {
	var item domain.AdvisorAction
	err := s.pool.QueryRow(ctx, `
		UPDATE advisor_actions SET applied=true WHERE id=$1 AND user_id=$2
		RETURNING id::text,kind,label,payload,applied`, actionID, userID).
		Scan(&item.ID, &item.Kind, &item.Label, &item.Payload, &item.Applied)
	return item, mapNotFound(err)
}

func (s *Store) TrackProductEvent(ctx context.Context, userID string, input domain.ProductEventInput) error {
	payload := input.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO product_events(user_id,name,payload) VALUES($1,$2,$3)`, userID, input.Name, payload)
	return err
}
