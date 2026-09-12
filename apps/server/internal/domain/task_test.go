package domain_test

import (
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestTaskStateClassification(t *testing.T) {
	if domain.TaskStatus("queued").Terminal() {
		t.Fatal("queued must not be terminal")
	}
	if domain.TaskStatus("leased").Terminal() {
		t.Fatal("leased must not be terminal")
	}
	if !domain.TaskStatus("succeeded").Terminal() {
		t.Fatal("succeeded must be terminal")
	}
	if !domain.TaskStatus("failed").Terminal() {
		t.Fatal("failed must be terminal")
	}
	if !domain.TaskStatus("cancelled").Terminal() {
		t.Fatal("cancelled must be terminal")
	}
	if !domain.TaskStatus("superseded").Terminal() {
		t.Fatal("superseded must be terminal")
	}
	if !domain.ErrorTransient.Retryable() {
		t.Fatal("transient must be retryable")
	}
	if !domain.ErrorThrottled.Retryable() {
		t.Fatal("throttled must be retryable")
	}
	if domain.ErrorPermanent.Retryable() {
		t.Fatal("permanent must not be retryable")
	}
	if domain.ErrorQualityRejected.Retryable() {
		t.Fatal("quality_rejected must not be retryable")
	}
	if domain.ErrorSuperseded.Retryable() {
		t.Fatal("superseded must not be retryable")
	}
}
