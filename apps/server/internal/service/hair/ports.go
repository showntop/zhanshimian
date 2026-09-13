package hair

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Grounding 是发型建议/预览所需的质量核心 grounding：报告发现 + 正脸媒体引用。
type Grounding struct {
	ReportID string
	Findings []domain.FindingGrounding
	Face     domain.MediaInput
}

type Reader interface {
	ReadHairGrounding(ctx context.Context, userID string, reportID string) (Grounding, error)
}
