package diagnostic

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Grounding 是 Outfit/Purchase 诊断所需的只读 grounding：报告 + 档案 + 衣橱。
// kind 是诊断类型（outfit/purchase），由调用方显式传入，不在 Reader 内部猜测。
type Grounding struct {
	Report   *domain.ReportGrounding
	Profile  domain.ProfileSnapshot
	Wardrobe []domain.WardrobeGroundingItem
}

type Reader interface {
	ReadDiagnosticGrounding(ctx context.Context, userID string, reportID string, kind string) (Grounding, error)
}
