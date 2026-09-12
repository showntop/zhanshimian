package provider

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/textproto"

	"github.com/zhanshimian/server/internal/domain"
)

type HairPreviewOutput struct {
	ImageData       []byte
	MIMEType        string
	ImageURL        string
	ProviderVersion string
}

type HairPreviewGenerator interface {
	Generate(context.Context, domain.HairPreviewInput) (HairPreviewOutput, error)
}

type DemoHairGenerator struct{}

func NewDemoHairGenerator() *DemoHairGenerator { return &DemoHairGenerator{} }

func (*DemoHairGenerator) Generate(_ context.Context, input domain.HairPreviewInput) (HairPreviewOutput, error) {
	paths := map[string]string{"sharp": "/assets/looks/sharp.png", "warm": "/assets/looks/warm.png", "natural": "/assets/looks/natural.png"}
	path := paths[input.StyleID]
	if path == "" {
		return HairPreviewOutput{}, fmt.Errorf("unsupported hair style")
	}
	return HairPreviewOutput{ImageURL: path, ProviderVersion: "demo-hair-v1"}, nil
}

func writeImagePart(writer *multipart.Writer, image AnalysisImage) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="source.jpg"`)
	header.Set("Content-Type", image.MIMEType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(image.Data)
	return err
}

func hairPrompt(styleID string) string {
	styles := map[string]string{
		"sharp":   "锁骨长度的轻盈层次发，颅顶自然蓬松，6:4 侧分，脸侧有少量空气感碎发，发尾干净利落",
		"warm":    "锁骨长度的空气微卷，柔和的大弧度发尾，颅顶自然蓬松，整体温柔明亮",
		"natural": "保持当前发长和发色，只调整为自然偏分并整理耳侧线条，低维护、真实日常",
	}
	return "仅编辑人物发型为：" + styles[styleID] + "。" + identityLockClause + "妆容、身体、服装、姿势、背景、光线和镜头构图保持不变；不添加首饰或文字。"
}

