package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/hair"
)

// fakeHairService 记录调用形状（过滤器、方向、次数），返回固定预览。
type fakeHairService struct {
	mu          sync.Mutex
	createCalls int
	lastFilter  hair.ListFilter
	lastInput   hair.CreatePreviewInput
}

func (f *fakeHairService) CreatePreview(_ context.Context, userID string, input hair.CreatePreviewInput) (hair.Preview, domain.OperationRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.lastInput = input
	// 与生产 service 同规则：自定义方向必须带描述
	if input.StyleID == hair.CustomDirectionID && input.Direction == "" {
		return hair.Preview{}, domain.OperationRef{}, hair.ErrDirectionInvalid
	}
	preview := hair.Preview{
		ID: "11111111-1111-1111-1111-111111111111", StyleID: input.StyleID, State: hair.StateQueued,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	return preview, domain.OperationRef{ID: "22222222-2222-2222-2222-222222222222", Kind: domain.OperationRender, Status: domain.OperationAccepted}, nil
}

func (f *fakeHairService) Get(_ context.Context, _ string, id string) (hair.Preview, error) {
	return hair.Preview{ID: id, State: hair.StateQueued}, nil
}

func (f *fakeHairService) Active(_ context.Context, _ string) (hair.Preview, error) {
	return hair.Preview{ID: "11111111-1111-1111-1111-111111111111", State: hair.StateQueued}, nil
}

func (f *fakeHairService) List(_ context.Context, _ string, filter hair.ListFilter) ([]hair.Preview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastFilter = filter
	return []hair.Preview{}, nil
}

func (f *fakeHairService) Save(_ context.Context, _ string, id string) (hair.Preview, error) {
	return hair.Preview{ID: id, State: hair.StateReady, Saved: true}, nil
}

func (f *fakeHairService) Recommend(_ context.Context, _ string, _ string) ([]domain.HairStyle, error) {
	return []domain.HairStyle{}, nil
}

func newHairHTTP(t *testing.T) (*fakeHairService, *assessmentHTTP) {
	t.Helper()
	fake := &fakeHairService{}
	api := &API{
		hair:        fake,
		idempotency: newMemoryIdempotencyStore(),
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	// 与生产注册同形：POST 强制 Idempotency-Key。
	mux.Handle("POST /v1/hair-previews", api.requireIdempotency(http.HandlerFunc(api.createHairPreview)))
	mux.HandleFunc("GET /v1/hair-previews", api.listHairPreviews)
	return fake, &assessmentHTTP{t: t, handler: mux, userID: "user-hair"}
}

func TestCreateHairPreviewRequiresIdempotencyKey(t *testing.T) {
	fake, api := newHairHTTP(t)
	body := `{"media_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","style_id":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}`

	res := api.Do(http.MethodPost, "/v1/hair-previews", body, nil)
	assertError(t, res, 400, "idempotency_key_required", false)

	res = api.Do(http.MethodPost, "/v1/hair-previews", body, map[string]string{"Idempotency-Key": "hair-1"})
	assertStatus(t, res, 202)
	assertJSONPath(t, res, "operation.kind", "render")
	assertJSONPath(t, res, "data.state", "queued")

	// 双击/重试同键同请求：重放已存响应，不重复创建。
	res = api.Do(http.MethodPost, "/v1/hair-previews", body, map[string]string{"Idempotency-Key": "hair-1"})
	assertStatus(t, res, 202)
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 (idempotent replay)", fake.createCalls)
	}
}

// 方向描述要原样进服务层（展示名 + 生成提示词），自定义方向缺描述按 400 拒绝。
func TestCreateHairPreviewPassesDirection(t *testing.T) {
	fake, api := newHairHTTP(t)

	res := api.Do(http.MethodPost, "/v1/hair-previews",
		`{"media_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","style_id":"custom","direction":"两侧推短，顶部留一点长度"}`,
		map[string]string{"Idempotency-Key": "hair-direction"})
	assertStatus(t, res, 202)
	if fake.lastInput.Direction != "两侧推短，顶部留一点长度" || fake.lastInput.StyleID != "custom" {
		t.Fatalf("direction 未透传: %#v", fake.lastInput)
	}

	res = api.Do(http.MethodPost, "/v1/hair-previews",
		`{"media_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","style_id":"custom"}`,
		map[string]string{"Idempotency-Key": "hair-direction-empty"})
	assertError(t, res, 400, "validation_error", false)
}

func TestListHairPreviewsQueryFilters(t *testing.T) {
	fake, api := newHairHTTP(t)

	res := api.Do(http.MethodGet, "/v1/hair-previews", "", nil)
	assertStatus(t, res, 200)
	if fake.lastFilter.Saved != nil || fake.lastFilter.Active {
		t.Fatalf("unfiltered list got filter %#v", fake.lastFilter)
	}

	res = api.Do(http.MethodGet, "/v1/hair-previews?saved=true", "", nil)
	assertStatus(t, res, 200)
	if fake.lastFilter.Saved == nil || !*fake.lastFilter.Saved || fake.lastFilter.Active {
		t.Fatalf("saved=true got filter %#v", fake.lastFilter)
	}

	res = api.Do(http.MethodGet, "/v1/hair-previews?state=active", "", nil)
	assertStatus(t, res, 200)
	if !fake.lastFilter.Active || fake.lastFilter.Saved != nil {
		t.Fatalf("state=active got filter %#v", fake.lastFilter)
	}

	res = api.Do(http.MethodGet, "/v1/hair-previews?state=ready", "", nil)
	assertError(t, res, 400, "validation_error", false)

	res = api.Do(http.MethodGet, "/v1/hair-previews?saved=notabool", "", nil)
	assertError(t, res, 400, "validation_error", false)
}
