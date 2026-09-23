package media

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/zhanshimian/server/internal/domain"
)

var (
	ErrValidation             = errors.New("validation error")
	ErrUploadMetadataMismatch = errors.New("upload metadata mismatch")
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type CreateIntentInput struct {
	Purpose  domain.MediaPurpose
	MIMEType string
	ByteSize int64
	SHA256   string
}

func validateCreate(in CreateIntentInput, maxBytes int64) error {
	switch in.Purpose {
	case domain.MediaPurposeFace, domain.MediaPurposeSide, domain.MediaPurposeBody, domain.MediaPurposeFeedback, domain.MediaPurposeWardrobe:
	default:
		return fmt.Errorf("%w: unsupported purpose", ErrValidation)
	}
	if in.MIMEType != "image/jpeg" && in.MIMEType != "image/png" {
		return fmt.Errorf("%w: only jpeg and png are supported", ErrValidation)
	}
	if in.ByteSize < 1 || in.ByteSize > maxBytes {
		return fmt.Errorf("%w: photo must be between 1 byte and 10 MB", ErrValidation)
	}
	if !sha256Pattern.MatchString(in.SHA256) {
		return fmt.Errorf("%w: sha256 must be 64 lowercase hex characters", ErrValidation)
	}
	return nil
}

func validateObject(intent domain.UploadIntent, meta domain.ObjectMetadata) error {
	// MIME 不在完整性校验内：声明值是客户端按扩展名猜的，以服务端嗅探为准。
	if intent.ObjectKey != meta.ObjectKey ||
		intent.ByteSize != meta.ByteSize || intent.SHA256 != meta.SHA256 {
		return ErrUploadMetadataMismatch
	}
	return nil
}
