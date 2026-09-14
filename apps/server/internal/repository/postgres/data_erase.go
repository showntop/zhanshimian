package postgres

import "context"

// 「删除我的数据」的行级删除清单（对象 key 由调用方回收）。
// 新 baseline 的外键级联已覆盖大部分路径；显式列出是为了确保
// users 行最后删除（会话先失效），且无孤儿业务行残留。
var deleteUserDataQueries = []string{
	`DELETE FROM billing_ledger WHERE user_id=$1`,
	`DELETE FROM billing_reservations WHERE user_id=$1`,
	`DELETE FROM billing_orders WHERE user_id=$1`,
	`DELETE FROM billing_wallets WHERE user_id=$1`,
	`DELETE FROM billing_usage WHERE user_id=$1`,
	// 手机号是 PII:sms_codes 按 phone 索引,须在身份行删除前清掉。
	`DELETE FROM sms_codes WHERE phone IN (SELECT identifier FROM user_identities WHERE user_id=$1 AND provider='phone')`,
	`DELETE FROM generation_feedback WHERE user_id=$1`,
	`DELETE FROM execution_feedback WHERE user_id=$1`,
	`DELETE FROM preference_memories WHERE user_id=$1`,
	`DELETE FROM execution_events WHERE user_id=$1`,
	`DELETE FROM execution_steps WHERE user_id=$1`,
	`DELETE FROM executions WHERE user_id=$1`,
	`DELETE FROM plan_selections WHERE user_id=$1`,
	`DELETE FROM shares WHERE user_id=$1`,
	`DELETE FROM today_plans WHERE user_id=$1`,
	`DELETE FROM tasks WHERE user_id=$1`,
	`DELETE FROM wardrobe_outfits WHERE user_id=$1`,
	`DELETE FROM wardrobe_items WHERE user_id=$1`,
	`DELETE FROM advisor_messages WHERE user_id=$1`,
	`DELETE FROM advisor_conversations WHERE user_id=$1`,
	`DELETE FROM diagnostics WHERE user_id=$1`,
	`DELETE FROM hair_previews WHERE user_id=$1`,
	`DELETE FROM product_events WHERE user_id=$1`,
	`DELETE FROM operations WHERE user_id=$1`,
	`DELETE FROM analysis_runs WHERE user_id=$1`,
	`DELETE FROM reports WHERE user_id=$1`,
	`DELETE FROM photo_sets WHERE user_id=$1`,
	`DELETE FROM plan_sets WHERE user_id=$1`,
	`DELETE FROM render_runs WHERE user_id=$1`,
	`DELETE FROM media_assets WHERE user_id=$1`,
	`DELETE FROM user_identities WHERE user_id=$1`,
	`DELETE FROM user_profiles WHERE user_id=$1`,
	`DELETE FROM user_sessions WHERE user_id=$1`,
	`DELETE FROM users WHERE id=$1`,
}

// DeleteUserData 删除该用户全部行级数据，返回需回收的 COS object key。
// 对象删除失败由上层只告警不阻塞（GC 队列兜底）。
func (s *Store) DeleteUserData(ctx context.Context, userID string) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// 新 baseline 只在 media_assets 存 object key；签名 URL 不落库。
	rows, err := tx.Query(ctx, `SELECT object_key FROM media_assets WHERE user_id=$1 AND deleted_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	rows.Close()
	for _, query := range deleteUserDataQueries {
		if _, err = tx.Exec(ctx, query, userID); err != nil {
			return nil, err
		}
	}
	return keys, tx.Commit(ctx)
}
