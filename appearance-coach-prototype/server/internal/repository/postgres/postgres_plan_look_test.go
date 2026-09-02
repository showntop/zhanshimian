package postgres

import (
	"errors"
	"testing"
)

func TestRetryPlanLookErrorStopsPermanentProviderFailures(t *testing.T) {
	permanent := []string{
		"aliyun returned status 403: AccessDenied.Unpurchased",
		"volcengine returned status 401: AuthenticationError",
		"generated image has unsupported format image/gif",
		"image response contained no usable image",
	}
	for _, message := range permanent {
		if retryPlanLookError(errors.New(message)) {
			t.Fatalf("expected permanent error to stop retrying: %q", message)
		}
	}
	if !retryPlanLookError(errors.New("aliyun request: context deadline exceeded")) {
		t.Fatal("transient provider error should be retried")
	}
}
