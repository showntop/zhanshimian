package postgres

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/testutil"
)

// 生产事故链（9.16，SQLSTATE 23502）：清单第 25 条 DELETE FROM tasks 经
// tasks→provider_invocations→media_assets 级联，触发 hair_previews 的复合外键
// ON DELETE SET NULL——不带列清单时 PG 把 user_id（NOT NULL）一起置空，
// 而 hair_previews 的行要到第 31 条才删，事务直接炸掉。
// 005 给全部复合 SET NULL 外键补上列清单后，这里必须跑通。
func TestDeleteUserDataWithHairPreviewChain(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('hair-user') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var operationID string
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO operations(user_id, kind, subject_type, subject_id, status)
		VALUES ($1::uuid, 'render', 'hair_preview', gen_random_uuid(), 'succeeded')
		RETURNING id::text`, userID).Scan(&operationID); err != nil {
		t.Fatal(err)
	}
	var taskID string
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO tasks(user_id, operation_id, type, subject_type, subject_id, payload_version, payload, dedupe_key, status, max_attempts, stage_code)
		VALUES ($1::uuid, $2::uuid, 'hair_preview', 'hair_preview', gen_random_uuid(), 1, '{}', 'probe-task', 'succeeded', 1, 'done')
		RETURNING id::text`, userID, operationID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	var invocationID string
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO provider_invocations(user_id, operation_id, task_id, attempt_no, capability, routing_config_version, provider_key, model_key, protocol, request_hash, status)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 0, 'hair_edit', 'probe#v1', 'probe', 'probe-model', 'openai_chat_completions', repeat('cd', 32), 'succeeded')
		RETURNING id::text`, userID, operationID, taskID).Scan(&invocationID); err != nil {
		t.Fatal(err)
	}
	var assetID string
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind, provider_invocation_id)
		VALUES ($1::uuid, 'user_upload', 'face', $2, repeat('ef', 32), 'image/jpeg', 1024, 'ready', 'original', $3::uuid)
		RETURNING id::text`, userID, "erase-hair-"+userID, invocationID).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO hair_previews(user_id, style_id, source_media_asset_id)
		VALUES ($1::uuid, 'probe-style', $2::uuid)`, userID, assetID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteUserData(ctx, userID); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}

	for _, table := range []string{"hair_previews", "media_assets", "provider_invocations", "tasks", "operations", "users"} {
		var n int
		col := "user_id"
		if table == "users" {
			col = "id"
		}
		if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE `+col+`=$1`, userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s rows remain after erase: %d", table, n)
		}
	}
}
