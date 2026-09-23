package ai

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// ReleaseConfig 是路由配置里的放量元数据:previous/candidate 两个代码版本、
// candidate 流量百分比和分桶盐。业务代码只读它做归因,不出现厂商或模型名。
type ReleaseConfig struct {
	PreviousVersion  string `json:"previous_version"`
	CandidateVersion string `json:"candidate_version"`
	CandidatePercent int    `json:"candidate_percent"`
	BucketSalt       string `json:"bucket_salt"`
}

// ValidateCandidatePercent 供 cmd/eval 等装配层复用同一份白名单。
func ValidateCandidatePercent(percent int) error {
	return validateCandidatePercent(percent)
}

// ReleaseBucket 供 provider 运行时在写台账时计算同一确定性桶号。
func ReleaseBucket(userID, salt string) int {
	return releaseBucket(userID, salt)
}

// validateCandidatePercent 只接受放量阶梯 0/5/25/50/100。
func validateCandidatePercent(percent int) error {
	switch percent {
	case 0, 5, 25, 50, 100:
		return nil
	default:
		return fmt.Errorf("candidate_percent must be one of 0, 5, 25, 50, 100")
	}
}

// releaseBucket 用 SHA-256(salt+":"+userID) 前 8 字节取模 100,同一用户
// 分桶永远确定;台账只记录 0-99 的桶号,不含任何用户标识。
func releaseBucket(userID, salt string) int {
	sum := sha256.Sum256([]byte(salt + ":" + userID))
	return int(binary.BigEndian.Uint64(sum[:8]) % 100)
}

// useCandidate 只有 bucket < candidate_percent 的用户走 candidate 版本。
func useCandidate(userID string, release ReleaseConfig) bool {
	return releaseBucket(userID, release.BucketSalt) < release.CandidatePercent
}
