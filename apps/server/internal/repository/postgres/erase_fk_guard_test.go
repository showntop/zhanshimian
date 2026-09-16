package postgres

import (
	"context"
	"regexp"
	"testing"

	"github.com/zhanshimian/server/internal/testutil"
)

// 002 的 body_presentations 对 media_assets 是 NO ACTION 外键（body/face_media_id）。
// 清除清单漏掉它时（3f0092a 之前），显式 DELETE media_assets 会撞 23503——
// users 的 CASCADE 兜底来不及：users 最后才删，删 media_assets 时这些行还在。
// 这里用真实外键链实跑一次，钉住这个生产事故（002 落地到清单修复之间破了三天）。
func TestDeleteUserDataWithBodyPresentationAssets(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('orbit-user') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	insertAsset := func(purpose string) string {
		var id string
		if err := store.pool.QueryRow(ctx, `
			INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
			VALUES ($1::uuid, 'user_upload', $2, $3, repeat('ab', 32), 'image/jpeg', 1024, 'ready', 'original')
			RETURNING id::text`,
			userID, purpose, "erase-"+purpose+"-"+userID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	bodyAssetID := insertAsset("body")
	faceAssetID := insertAsset("face")
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO body_presentations(user_id, body_media_id, face_media_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid)`, userID, bodyAssetID, faceAssetID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteUserData(ctx, userID); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}

	for _, table := range []string{"body_presentations", "media_assets", "users"} {
		var n int
		if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE `+
			map[string]string{"users": "id", "body_presentations": "user_id", "media_assets": "user_id"}[table]+`=$1`,
			userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s rows remain after erase: %d", table, n)
		}
	}
}

// SET NULL / SET DEFAULT 外键的置空列必须全部可空：
// 不带列清单（confdelsetcols 为空）时 PG 置空全部外键列——其中有 NOT NULL 列，
// 删被引用行时直接 23502（9.16 生产事故：hair_previews.user_id，005 修复）。
// 带列清单时只置空清单内的列，清单内列同样必须可空。
// 以后新增复合 SET NULL 外键忘了列清单，这条测试会先叫。
func TestSetNullForeignKeysNullOnlyNullableColumns(t *testing.T) {
	pool := testutil.NewPostgres(t)
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT con.conrelid::regclass::text, con.conname,
			COALESCE((
				SELECT string_agg(a.attname, ',')
				FROM pg_attribute a
				WHERE a.attrelid = con.conrelid AND a.attnotnull
					AND a.attnum = ANY(
						CASE WHEN con.confdelsetcols IS NULL OR con.confdelsetcols::smallint[] = '{}'::smallint[]
							THEN con.conkey
							ELSE con.confdelsetcols::smallint[] END)
			), '') AS notnull_nulled
		FROM pg_constraint con
		JOIN pg_namespace n ON n.oid = con.connamespace
		WHERE con.contype = 'f' AND con.confdeltype IN ('n', 'd') AND n.nspname = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var child, name, notnullNulled string
		if err := rows.Scan(&child, &name, &notnullNulled); err != nil {
			t.Fatal(err)
		}
		if notnullNulled != "" {
			t.Fatalf("%s 的 SET NULL/SET DEFAULT 外键 %s 会置空 NOT NULL 列（%s）：删被引用行时 23502——复合外键必须带列清单（见 005）",
				child, name, notnullNulled)
		}
	}
}

// 注销清除清单的两条结构硬约束（9.15 生产 42P01 / 23503 事故的教训）：
// 1. 清单引用的表必须真实存在——库滞后于清单时 DELETE 直接 42P01；
// 2. 任何对「清单表 / users」持有 NO ACTION 或 RESTRICT 外键的表，
//    必须也在清单里且排在被引用表之前——只靠 users 级联兜底来不及，
//    users 最后删，显式删被引用表时这些行还在（body_presentations 事故）。
// 新加用户表 / 新加外键时这条测试会先叫，不再靠生产事故发现。
func TestDeleteUserDataRespectsForeignKeyOrder(t *testing.T) {
	pool := testutil.NewPostgres(t)
	ctx := context.Background()

	tableName := regexp.MustCompile(`DELETE FROM (\w+)`)
	inList := map[string]int{}
	for i, query := range deleteUserDataQueries {
		inList[tableName.FindStringSubmatch(query)[1]] = i
	}

	// 约束 1：42P01 防护
	for name := range inList {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("deleteUserDataQueries references missing table %s", name)
		}
	}

	// 约束 2：NO ACTION('a') / RESTRICT('r') 外键的 child 必须更早被清；
	// INITIALLY DEFERRED 的约束（condeferred）在 COMMIT 才校验，届时双方都已删，不算违例
	rows, err := pool.Query(ctx, `
		SELECT con.conrelid::regclass::text, con.confrelid::regclass::text, con.confdeltype::text
		FROM pg_constraint con
		JOIN pg_namespace n ON n.oid = con.connamespace
		WHERE con.contype = 'f' AND con.confdeltype IN ('a', 'r') AND NOT con.condeferred
			AND n.nspname = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var child, parent, deltype string
		if err := rows.Scan(&child, &parent, &deltype); err != nil {
			t.Fatal(err)
		}
		parentPos, parentErased := inList[parent]
		if !parentErased {
			continue
		}
		childPos, childErased := inList[child]
		if !childErased {
			t.Fatalf("%s 对 %s 持有 NO ACTION/RESTRICT 外键却不在注销清除清单里：显式删 %s 时它的行还在，会 23503",
				child, parent, parent)
		}
		if childPos > parentPos {
			t.Fatalf("%s 在清除清单里排在 %s 之后，但它对 %s 是 NO ACTION/RESTRICT 外键：删 %s 时 23503",
				child, parent, parent, parent)
		}
	}
}
