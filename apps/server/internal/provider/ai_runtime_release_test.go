package provider

import (
	"testing"

	providerai "github.com/zhanshimian/server/internal/provider/ai"
)

// 配置 release 块后,台账行必须带上该用户的确定性分桶号:同一用户同一盐
// 桶号恒定,且桶号本身不含任何用户标识(0-99 整数)。
func TestAIRuntimeRecordsReleaseBucketWhenConfigured(t *testing.T) {
	store := &invocationStoreFake{startID: "inv-1"}
	server := chatServer(t, "req-bucket")
	defer server.Close()
	runtime := newRecordingRuntime(t, store, server)
	runtime.SetRelease(providerai.ReleaseConfig{
		PreviousVersion: "quality-core-r1", CandidateVersion: "quality-core-r2",
		CandidatePercent: 5, BucketSalt: "quality-core-2026-09-12",
	})

	if _, err := runtime.Structured(scopedInvocationCtx(), CapabilityAppearanceAnalysis,
		StructuredRequest{Prompt: "p", SchemaName: "report", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 100}); err != nil {
		t.Fatal(err)
	}
	if len(store.starts) != 1 {
		t.Fatalf("expected one ledger row, got %d", len(store.starts))
	}
	bucket := store.starts[0].ReleaseBucket
	if bucket == nil {
		t.Fatal("release bucket must be recorded when release is configured")
	}
	want := providerai.ReleaseBucket("user-1", "quality-core-2026-09-12")
	if *bucket != want {
		t.Fatalf("bucket = %d, want deterministic %d", *bucket, want)
	}
	if *bucket < 0 || *bucket > 99 {
		t.Fatalf("bucket out of range: %d", *bucket)
	}
}

// 未配置 release 时台账不记桶号,保持 nullable 语义。
func TestAIRuntimeOmitsReleaseBucketWithoutRelease(t *testing.T) {
	store := &invocationStoreFake{startID: "inv-1"}
	server := chatServer(t, "req-no-bucket")
	defer server.Close()
	runtime := newRecordingRuntime(t, store, server)

	if _, err := runtime.Structured(scopedInvocationCtx(), CapabilityAppearanceAnalysis,
		StructuredRequest{Prompt: "p", SchemaName: "report", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 100}); err != nil {
		t.Fatal(err)
	}
	if len(store.starts) != 1 {
		t.Fatalf("expected one ledger row, got %d", len(store.starts))
	}
	if store.starts[0].ReleaseBucket != nil {
		t.Fatalf("bucket must stay nil without release config, got %d", *store.starts[0].ReleaseBucket)
	}
}
