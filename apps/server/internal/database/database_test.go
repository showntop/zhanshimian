package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestBaselineCreatesQualityCoreSchema(t *testing.T) {
	pool := testutil.NewPostgres(t)
	expected := []string{
		"users", "user_identities", "user_sessions", "user_profiles",
		"media_assets", "upload_intents", "photo_sets", "photo_set_items",
		"operations", "tasks", "provider_invocations", "idempotency_keys",
		"analysis_runs", "reports", "report_findings",
		"plan_sets", "plan_variants", "plan_steps", "plan_step_groundings",
		"render_specs", "render_heads", "render_runs", "render_candidates",
		"quality_evaluations", "render_publications",
		"plan_selections", "executions", "execution_steps", "execution_events",
		"generation_feedback", "execution_feedback", "object_gc_jobs",
		"billing_wallets", "billing_reservations", "billing_ledger",
	}
	for _, table := range expected {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("missing table %s", table)
		}
	}
}

func TestBaselineRejectsCrossUserMediaAttachment(t *testing.T) {
	pool := testutil.NewPostgres(t)
	userA := insertUser(t, pool)
	userB := insertUser(t, pool)
	asset := insertMedia(t, pool, userA, "user_upload", "face")
	photoSet := insertPhotoSet(t, pool, userB)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO photo_set_items(user_id,photo_set_id,role,media_asset_id)
		VALUES($1,$2,'face',$3)`, userB, photoSet, asset)
	if err == nil {
		t.Fatal("expected cross-user media attachment to fail")
	}
	pgErr, ok := err.(*pgconn.PgError)
	if !ok {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
	}
	if pgErr.Code != "23503" {
		t.Fatalf("expected SQLSTATE 23503, got %s", pgErr.Code)
	}
}

func TestBaselineMigrateIsIdempotent(t *testing.T) {
	pool := testutil.NewPostgres(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM schema_migrations WHERE version=$1`, "001_baseline.sql").Scan(&n)
	if err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected one schema_migrations row for 001_baseline.sql, got %d", n)
	}
}

func TestBaselineDeleteUserWithCurrentReport(t *testing.T) {
	pool := testutil.NewPostgres(t)
	fix := insertReportGraph(t, pool)
	if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, fix.userID); err != nil {
		t.Fatalf("delete user with current_report_id set: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, fix.userID).Scan(&exists); err != nil {
		t.Fatalf("check user deleted: %v", err)
	}
	if exists {
		t.Fatal("expected user row to be deleted")
	}
}

func TestBaselineDeleteReportNullsCurrentPointer(t *testing.T) {
	pool := testutil.NewPostgres(t)
	fix := insertReportGraph(t, pool)
	if _, err := pool.Exec(context.Background(), `DELETE FROM reports WHERE id=$1`, fix.reportID); err != nil {
		t.Fatalf("delete report: %v", err)
	}
	var current *uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT current_report_id FROM user_profiles WHERE id=$1`, fix.profileID).Scan(&current); err != nil {
		t.Fatalf("profile should remain after report delete: %v", err)
	}
	if current != nil {
		t.Fatalf("expected current_report_id NULL, got %s", current)
	}
}

func TestBaselineDeletePublicationNullsHeadPointer(t *testing.T) {
	pool := testutil.NewPostgres(t)
	fix := insertPublicationGraph(t, pool)
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM render_publications WHERE id=$1`, fix.publicationID); err != nil {
		t.Fatalf("delete publication: %v", err)
	}
	var current *uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT current_publication_id FROM render_heads WHERE user_id=$1 AND plan_variant_id=$2`,
		fix.userID, fix.variantID).Scan(&current); err != nil {
		t.Fatalf("render head should remain after publication delete: %v", err)
	}
	if current != nil {
		t.Fatalf("expected current_publication_id NULL, got %s", current)
	}
}

func TestBaselineRejectsImmutableUpdate(t *testing.T) {
	pool := testutil.NewPostgres(t)
	fix := insertReportGraph(t, pool)
	_, err := pool.Exec(context.Background(),
		`UPDATE reports SET priority_title='changed' WHERE id=$1`, fix.reportID)
	if err == nil {
		t.Fatal("expected update of immutable reports to fail")
	}
	pgErr, ok := err.(*pgconn.PgError)
	if !ok {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
	}
	if pgErr.Code != "55000" {
		t.Fatalf("expected SQLSTATE 55000, got %s: %s", pgErr.Code, pgErr.Message)
	}
}

func insertUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO users DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func insertMedia(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, origin, purpose string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	objectKey := "users/" + userID.String() + "/uploads/" + uuid.NewString()
	sha := strings.Repeat("ab", 32)
	err := pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(
			user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind
		) VALUES ($1,$2,$3,$4,$5,'image/jpeg',128,'ready','original')
		RETURNING id`, userID, origin, purpose, objectKey, sha).Scan(&id)
	if err != nil {
		t.Fatalf("insert media: %v", err)
	}
	return id
}

func insertPhotoSet(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO photo_sets(user_id, profile_snapshot, content_hash, schema_version)
		VALUES ($1,'{}'::jsonb,$2,'v1')
		RETURNING id`, userID, hex64()).Scan(&id)
	if err != nil {
		t.Fatalf("insert photo set: %v", err)
	}
	return id
}

type reportGraph struct {
	userID    uuid.UUID
	profileID uuid.UUID
	reportID  uuid.UUID
	photoSet  uuid.UUID
	invID     uuid.UUID
}

type publicationGraph struct {
	userID        uuid.UUID
	variantID     uuid.UUID
	publicationID uuid.UUID
}

func hex64() string {
	return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")[:64]
}

func insertOperation(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, kind, subjectType string, subjectID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO operations(user_id,kind,subject_type,subject_id,status,stage_code)
		VALUES ($1,$2,$3,$4,'succeeded','done')
		RETURNING id`, userID, kind, subjectType, subjectID).Scan(&id)
	if err != nil {
		t.Fatalf("insert operation: %v", err)
	}
	return id
}

func insertTask(t *testing.T, pool *pgxpool.Pool, userID, operationID, subjectID uuid.UUID, typ, subjectType, dedupe string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tasks(
			user_id,operation_id,type,subject_type,subject_id,
			payload_version,payload,dedupe_key,status,max_attempts,stage_code
		) VALUES ($1,$2,$3,$4,$5,1,'{}'::jsonb,$6,'succeeded',3,'done')
		RETURNING id`, userID, operationID, typ, subjectType, subjectID, dedupe).Scan(&id)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return id
}

func insertInvocation(t *testing.T, pool *pgxpool.Pool, userID, operationID, taskID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO provider_invocations(
			user_id,operation_id,task_id,attempt_no,capability,
			routing_config_version,provider_key,model_key,protocol,request_hash,status
		) VALUES ($1,$2,$3,0,'vision','v1','p','m','http',$4,'succeeded')
		RETURNING id`, userID, operationID, taskID, hex64()).Scan(&id)
	if err != nil {
		t.Fatalf("insert invocation: %v", err)
	}
	return id
}

func insertEval(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, subjectType string, subjectID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO quality_evaluations(user_id,subject_type,subject_id,policy_version,decision)
		VALUES ($1,$2,$3,'v1','pass')
		RETURNING id`, userID, subjectType, subjectID).Scan(&id)
	if err != nil {
		t.Fatalf("insert quality evaluation: %v", err)
	}
	return id
}

func insertReportGraph(t *testing.T, pool *pgxpool.Pool) reportGraph {
	t.Helper()
	userID := insertUser(t, pool)
	photoSet := insertPhotoSet(t, pool, userID)
	asset := insertMedia(t, pool, userID, "user_upload", "face")
	opID := insertOperation(t, pool, userID, "assessment", "photo_set", photoSet)
	taskID := insertTask(t, pool, userID, opID, photoSet, "analyze", "photo_set", "assess-"+userID.String())
	invID := insertInvocation(t, pool, userID, opID, taskID)
	evalID := insertEval(t, pool, userID, "report", uuid.New())
	var reportID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO reports(
			user_id,photo_set_id,profile_snapshot,impression_tags,priority_title,priority_copy,
			hero_asset_id,schema_version,content_hash,provider_invocation_id,quality_evaluation_id
		) VALUES ($1,$2,'{}'::jsonb,'{}','title','copy',$3,'v1',$4,$5,$6)
		RETURNING id`, userID, photoSet, asset, hex64(), invID, evalID).Scan(&reportID)
	if err != nil {
		t.Fatalf("insert report: %v", err)
	}
	var profileID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO user_profiles(user_id,current_report_id) VALUES ($1,$2) RETURNING id`,
		userID, reportID).Scan(&profileID)
	if err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return reportGraph{userID: userID, profileID: profileID, reportID: reportID, photoSet: photoSet, invID: invID}
}

func insertCompletePlanSet(t *testing.T, pool *pgxpool.Pool, userID, reportID, photoSet, invID uuid.UUID) (planSetID, variantID, specID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin plan set tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	evalID := insertEval(t, pool, userID, "plan_set", uuid.New())
	err = tx.QueryRow(ctx, `
		INSERT INTO plan_sets(
			user_id,report_id,profile_snapshot,scene,scene_brief,brief_hash,
			planner_schema_version,style_rule_version,provider_invocation_id,quality_evaluation_id
		) VALUES ($1,$2,'{}'::jsonb,'daily','{}'::jsonb,$3,'v1','v1',$4,$5)
		RETURNING id`, userID, reportID, hex64(), invID, evalID).Scan(&planSetID)
	if err != nil {
		t.Fatalf("insert plan set: %v", err)
	}

	keys := []string{"sharp", "warm", "natural"}
	for i, key := range keys {
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO plan_variants(
				user_id,plan_set_id,slot,key,name,descriptor,rationale,recommended,outcome_tags,difference_tags
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'{}','{}')
			RETURNING id`, userID, planSetID, i+1, key, key+" look", key+" descriptor text", key+" rationale text here", i == 0).Scan(&id)
		if err != nil {
			t.Fatalf("insert plan variant %s: %v", key, err)
		}
		if i == 0 {
			variantID = id
		}
		categories := []string{"hair", "makeup", "outfit"}
		for pos, category := range categories {
			var stepID uuid.UUID
			err = tx.QueryRow(ctx, `
				INSERT INTO plan_steps(user_id,plan_variant_id,category,action,title,summary,details,position)
				VALUES ($1,$2,$3,'keep',$4,$5,'{}'::jsonb,$6)
				RETURNING id`, userID, id, category, "keep "+category+" style", "keep the current "+category+" unchanged", pos+1).Scan(&stepID)
			if err != nil {
				t.Fatalf("insert plan step %s/%s: %v", key, category, err)
			}
			if _, err = tx.Exec(ctx, `
				INSERT INTO plan_step_groundings(user_id,plan_step_id,source_type,source_id,reason)
				VALUES ($1,$2,'style_rule','rule-1','matches the selected style')`, userID, stepID); err != nil {
				t.Fatalf("insert grounding: %v", err)
			}
		}
		var createdSpec uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO render_specs(user_id,plan_variant_id,source_photo_set_id,schema_version,spec,content_hash)
			VALUES ($1,$2,$3,'v1','{}'::jsonb,$4)
			RETURNING id`, userID, id, photoSet, hex64()).Scan(&createdSpec)
		if err != nil {
			t.Fatalf("insert render spec: %v", err)
		}
		if i == 0 {
			specID = createdSpec
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatalf("commit plan set: %v", err)
	}
	return planSetID, variantID, specID
}

func insertPublicationGraph(t *testing.T, pool *pgxpool.Pool) publicationGraph {
	t.Helper()
	core := insertReportGraph(t, pool)
	_, variantID, specID := insertCompletePlanSet(t, pool, core.userID, core.reportID, core.photoSet, core.invID)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO render_heads(user_id,plan_variant_id) VALUES ($1,$2)`, core.userID, variantID); err != nil {
		t.Fatalf("insert render head: %v", err)
	}
	opID := insertOperation(t, pool, core.userID, "render", "plan_variant", variantID)
	taskID := insertTask(t, pool, core.userID, opID, variantID, "render", "plan_variant", "render-"+variantID.String())
	invID := insertInvocation(t, pool, core.userID, opID, taskID)
	var runID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO render_runs(
			user_id,plan_variant_id,render_spec_id,generation,operation_id,
			routing_policy_version,quality_policy_version
		) VALUES ($1,$2,$3,1,$4,'v1','v1')
		RETURNING id`, core.userID, variantID, specID, opID).Scan(&runID)
	if err != nil {
		t.Fatalf("insert render run: %v", err)
	}
	var assetID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(
			user_id,origin,purpose,object_key,sha256,mime_type,byte_size,state,display_kind,provider_invocation_id
		) VALUES ($1,'provider_output','render_candidate',$2,$3,'image/jpeg',128,'quarantined','generated_reference',$4)
		RETURNING id`, core.userID, "users/"+core.userID.String()+"/renders/"+uuid.NewString(), hex64(), invID).Scan(&assetID)
	if err != nil {
		t.Fatalf("insert candidate media: %v", err)
	}
	var candidateID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO render_candidates(user_id,render_run_id,ordinal,asset_id,provider_invocation_id)
		VALUES ($1,$2,1,$3,$4)
		RETURNING id`, core.userID, runID, assetID, invID).Scan(&candidateID)
	if err != nil {
		t.Fatalf("insert render candidate: %v", err)
	}
	evalID := insertEval(t, pool, core.userID, "render_candidate", candidateID)
	var publicationID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO render_publications(
			user_id,plan_variant_id,render_run_id,candidate_id,quality_evaluation_id,generation
		) VALUES ($1,$2,$3,$4,$5,1)
		RETURNING id`, core.userID, variantID, runID, candidateID, evalID).Scan(&publicationID)
	if err != nil {
		t.Fatalf("insert publication: %v", err)
	}
	if _, err = pool.Exec(context.Background(), `
		UPDATE render_heads SET current_publication_id=$1
		WHERE user_id=$2 AND plan_variant_id=$3`, publicationID, core.userID, variantID); err != nil {
		t.Fatalf("point head at publication: %v", err)
	}
	return publicationGraph{userID: core.userID, variantID: variantID, publicationID: publicationID}
}
