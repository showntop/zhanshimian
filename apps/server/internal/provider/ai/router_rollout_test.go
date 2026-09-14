package ai

import "testing"

// 放量百分比只允许 0/5/25/50/100 五档,其他值一律拒绝——路由配置校验和
// set-rollout.mjs 共用同一份白名单,防止手写配置绕过放量阶梯。
func TestCandidatePercentAllowsOnlyReleaseSteps(t *testing.T) {
	for _, percent := range []int{0, 5, 25, 50, 100} {
		if err := validateCandidatePercent(percent); err != nil {
			t.Fatalf("percent %d: %v", percent, err)
		}
	}
	for _, percent := range []int{-1, 1, 10, 24, 51, 101} {
		if err := validateCandidatePercent(percent); err == nil {
			t.Fatalf("percent %d must be rejected", percent)
		}
	}
}

// 同一用户在同一 bucket_salt 下的分桶必须确定:配置重载、进程重启都不能
// 改变一个用户落在 candidate 还是 previous,否则放量观察窗的指标无法归因。
func TestReleaseBucketIsStablePerUser(t *testing.T) {
	first := releaseBucket("user-42", "quality-core-2026-09-12")
	for i := 0; i < 100; i++ {
		if got := releaseBucket("user-42", "quality-core-2026-09-12"); got != first {
			t.Fatalf("bucket changed: %d != %d", got, first)
		}
	}
}

// bucket < candidate_percent 才走 candidate;0% 无人走 candidate,100% 全部走。
func TestUseCandidateFollowsBucketBoundary(t *testing.T) {
	release := ReleaseConfig{CandidateVersion: "quality-core-r2", BucketSalt: "quality-core-2026-09-12"}
	release.CandidatePercent = 0
	for _, userID := range []string{"user-1", "user-42", "user-1000"} {
		if useCandidate(userID, release) {
			t.Fatalf("percent 0 must not route %s to candidate", userID)
		}
	}
	release.CandidatePercent = 100
	for _, userID := range []string{"user-1", "user-42", "user-1000"} {
		if !useCandidate(userID, release) {
			t.Fatalf("percent 100 must route %s to candidate", userID)
		}
	}
	release.CandidatePercent = 5
	if got := useCandidate("user-42", release); got != (releaseBucket("user-42", release.BucketSalt) < 5) {
		t.Fatalf("useCandidate disagrees with bucket boundary")
	}
}
