package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/testutil"
)

type photoAssets struct {
	FaceAssetID uuid.UUID
	SideAssetID uuid.UUID
	BodyAssetID uuid.UUID
}

func TestAssessmentSchemaRejectsCrossTenantAndMutations(t *testing.T) {
	db := testutil.NewPostgres(t)
	alice, bob := seedUsers(t, db)
	aliceAssets := seedPhotoAssets(t, db, alice)
	photoSetID := insertPhotoSet(t, db, alice, aliceAssets)

	_, err := db.Exec(context.Background(), `
		INSERT INTO photo_set_items(id,user_id,photo_set_id,role,media_asset_id)
		VALUES(gen_random_uuid(),$1,$2,'face',$3)`,
		bob, photoSetID, aliceAssets.FaceAssetID)
	// Foundation left the composite owner FK unnamed; default is not photo_set_items_photo_set_owner_fk.
	assertConstraint(t, err, "photo_set_items_user_id_photo_set_id_fkey")

	reportID := insertPublishedReport(t, db, alice, photoSetID, aliceAssets.BodyAssetID)
	_, err = db.Exec(context.Background(), `UPDATE reports SET priority_title='changed' WHERE id=$1`, reportID)
	assertImmutable(t, err)
	// Immutable means UPDATE is rejected (55000). DELETE stays allowed for user wipe / CASCADE.
	_, err = db.Exec(context.Background(), `UPDATE report_findings SET label='changed' WHERE report_id=$1`, reportID)
	assertImmutable(t, err)
}

func TestPhotoSetAndAnalysisInputHashesAreIdempotentPerUser(t *testing.T) {
	db := testutil.NewPostgres(t)
	userID := seedUser(t, db)
	assertUniqueConstraint(t, db, `photo_sets`, userID, "content_hash", strings.Repeat("a", 64))
	assertUniqueConstraint(t, db, `analysis_runs`, userID, "input_hash", strings.Repeat("b", 64))
}

func seedUsers(t *testing.T, db *pgxpool.Pool) (alice, bob uuid.UUID) {
	t.Helper()
	return seedUser(t, db), seedUser(t, db)
}

func seedUser(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(context.Background(), `INSERT INTO users(nickname) VALUES ('assessment-schema') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func seedPhotoAssets(t *testing.T, db *pgxpool.Pool, userID uuid.UUID) photoAssets {
	t.Helper()
	return photoAssets{
		FaceAssetID: insertSchemaMedia(t, db, userID, "face"),
		SideAssetID: insertSchemaMedia(t, db, userID, "side"),
		BodyAssetID: insertSchemaMedia(t, db, userID, "body"),
	}
}

func insertPhotoSet(t *testing.T, db *pgxpool.Pool, userID uuid.UUID, _ photoAssets) uuid.UUID {
	t.Helper()
	id, err := insertPhotoSetHash(db, userID, schemaHex64())
	if err != nil {
		t.Fatalf("insert photo set: %v", err)
	}
	return id
}

func insertPublishedReport(t *testing.T, db *pgxpool.Pool, userID, photoSetID, heroAssetID uuid.UUID) uuid.UUID {
	t.Helper()
	itemID := insertSchemaPhotoSetItem(t, db, userID, photoSetID, "body", heroAssetID)
	opID := insertSchemaOperation(t, db, userID, photoSetID)
	taskID := insertSchemaTask(t, db, userID, opID, photoSetID)
	invID := insertSchemaInvocation(t, db, userID, opID, taskID)
	evalID := insertSchemaEvaluation(t, db, userID)
	var reportID uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO reports(
			user_id,photo_set_id,profile_snapshot,impression_tags,priority_title,priority_copy,
			hero_asset_id,schema_version,content_hash,provider_invocation_id,quality_evaluation_id
		) VALUES ($1,$2,'{}'::jsonb,'{clean}','title','copy',$3,'v1',$4,$5,$6)
		RETURNING id`, userID, photoSetID, heroAssetID, schemaHex64(), invID, evalID).Scan(&reportID)
	if err != nil {
		t.Fatalf("insert report: %v", err)
	}
	_, err = db.Exec(context.Background(), `
		INSERT INTO report_findings(
			user_id,report_id,category,priority,label,visible_observation,recommendation,
			source_photo_item_id,anchor_x,anchor_y,anchor_w,anchor_h,confidence,position
		) VALUES ($1,$2,'hair',1,'label','visible fact','do this',$3,0.1,0.1,0.2,0.2,0.9,1)`,
		userID, reportID, itemID)
	if err != nil {
		t.Fatalf("insert report finding: %v", err)
	}
	return reportID
}

func assertConstraint(t *testing.T, err error, name string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected constraint %s to reject the statement", name)
	}
	pgErr := requirePgError(t, err)
	if pgErr.ConstraintName != name {
		t.Fatalf("constraint = %q, want %q (SQLSTATE %s: %s)", pgErr.ConstraintName, name, pgErr.Code, pgErr.Message)
	}
}

func assertImmutable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected immutable update to fail")
	}
	pgErr := requirePgError(t, err)
	if pgErr.Code != "55000" {
		t.Fatalf("expected SQLSTATE 55000, got %s: %s", pgErr.Code, pgErr.Message)
	}
	if !strings.Contains(pgErr.Message, "immutable relation") || !strings.Contains(pgErr.Message, "cannot be changed") {
		t.Fatalf("immutable message = %q", pgErr.Message)
	}
}

func assertUniqueConstraint(t *testing.T, db *pgxpool.Pool, table string, userID uuid.UUID, column, value string) {
	t.Helper()
	switch table {
	case "photo_sets":
		if _, err := insertPhotoSetHash(db, userID, value); err != nil {
			t.Fatalf("first photo_sets insert: %v", err)
		}
		_, err := insertPhotoSetHash(db, userID, value)
		assertConstraint(t, err, "photo_sets_user_id_content_hash_key")
		assertSQLState(t, err, "23505")
	case "analysis_runs":
		photoSetID, err := insertPhotoSetHash(db, userID, schemaHex64())
		if err != nil {
			t.Fatalf("analysis_runs photo set: %v", err)
		}
		opID := insertSchemaOperation(t, db, userID, photoSetID)
		if err := insertAnalysisRun(db, userID, photoSetID, opID, value); err != nil {
			t.Fatalf("first analysis_runs insert: %v", err)
		}
		err = insertAnalysisRun(db, userID, photoSetID, opID, value)
		assertConstraint(t, err, "analysis_runs_inflight_input_uidx")
		assertSQLState(t, err, "23505")
	default:
		t.Fatalf("unsupported table %s column %s", table, column)
	}
}

func requirePgError(t *testing.T, err error) *pgconn.PgError {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
	}
	return pgErr
}

func assertSQLState(t *testing.T, err error, code string) {
	t.Helper()
	pgErr := requirePgError(t, err)
	if pgErr.Code != code {
		t.Fatalf("SQLSTATE = %s, want %s: %s", pgErr.Code, code, pgErr.Message)
	}
}

func insertPhotoSetHash(db *pgxpool.Pool, userID uuid.UUID, contentHash string) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO photo_sets(user_id, profile_snapshot, content_hash, schema_version)
		VALUES ($1, '{}'::jsonb, $2, 'v1')
		RETURNING id`, userID, contentHash).Scan(&id)
	return id, err
}

func insertAnalysisRun(db *pgxpool.Pool, userID, photoSetID, operationID uuid.UUID, inputHash string) error {
	_, err := db.Exec(context.Background(), `
		INSERT INTO analysis_runs(
			user_id,photo_set_id,operation_id,input_hash,profile_snapshot,
			analyzer_schema_version,quality_policy_version
		) VALUES ($1,$2,$3,$4,'{}'::jsonb,'v1','v1')`, userID, photoSetID, operationID, inputHash)
	return err
}

func insertSchemaMedia(t *testing.T, db *pgxpool.Pool, userID uuid.UUID, purpose string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO media_assets(
			user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind
		) VALUES ($1,'user_upload',$2,$3,$4,'image/jpeg',128,'ready','original')
		RETURNING id`, userID, purpose, "users/"+userID.String()+"/uploads/"+uuid.NewString(), schemaHex64()).Scan(&id)
	if err != nil {
		t.Fatalf("insert media %s: %v", purpose, err)
	}
	return id
}

func insertSchemaPhotoSetItem(t *testing.T, db *pgxpool.Pool, userID, photoSetID uuid.UUID, role string, assetID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO photo_set_items(user_id,photo_set_id,role,media_asset_id)
		VALUES ($1,$2,$3,$4)
		RETURNING id`, userID, photoSetID, role, assetID).Scan(&id)
	if err != nil {
		t.Fatalf("insert photo set item %s: %v", role, err)
	}
	return id
}

func insertSchemaOperation(t *testing.T, db *pgxpool.Pool, userID, subjectID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO operations(user_id,kind,subject_type,subject_id,status,stage_code)
		VALUES ($1,'assessment','photo_set',$2,'succeeded','done')
		RETURNING id`, userID, subjectID).Scan(&id)
	if err != nil {
		t.Fatalf("insert operation: %v", err)
	}
	return id
}

func insertSchemaTask(t *testing.T, db *pgxpool.Pool, userID, operationID, subjectID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO tasks(
			user_id,operation_id,type,subject_type,subject_id,
			payload_version,payload,dedupe_key,status,max_attempts,stage_code
		) VALUES ($1,$2,'analyze','photo_set',$3,1,'{}'::jsonb,$4,'succeeded',3,'done')
		RETURNING id`, userID, operationID, subjectID, uuid.NewString()).Scan(&id)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return id
}

func insertSchemaInvocation(t *testing.T, db *pgxpool.Pool, userID, operationID, taskID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO provider_invocations(
			user_id,operation_id,task_id,attempt_no,capability,
			routing_config_version,provider_key,model_key,protocol,request_hash,status
		) VALUES ($1,$2,$3,0,'vision','v1','p','m','http',$4,'succeeded')
		RETURNING id`, userID, operationID, taskID, schemaHex64()).Scan(&id)
	if err != nil {
		t.Fatalf("insert invocation: %v", err)
	}
	return id
}

func insertSchemaEvaluation(t *testing.T, db *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO quality_evaluations(user_id,subject_type,subject_id,policy_version,decision)
		VALUES ($1,'report',$2,'v1','pass')
		RETURNING id`, userID, uuid.New()).Scan(&id)
	if err != nil {
		t.Fatalf("insert quality evaluation: %v", err)
	}
	return id
}

func schemaHex64() string {
	return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")[:64]
}
