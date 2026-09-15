package ai

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png"
)

const (
	visionMaxEdge      = 1280
	visionJPEGQuality  = 75
	visionKeepMaxBytes = 400 << 10
	// VisionCOSProcess 是数据万象下载时处理规则：长边 1280、JPEG 75。
	// 未开通 CI 时 OpenProcessed 失败，调用方回退本地缩放（ConstrainVisionImage）。
	VisionCOSProcess = "imageMogr2/thumbnail/1280x/format/jpg/quality/75"

	// 本人图编辑要比分析多留五官细节；仍封顶以免撑爆生成超时。
	editMaxEdge      = 1536
	editJPEGQuality  = 85
	editKeepMaxBytes = 800 << 10
	// EditCOSProcess 是编辑/渲染参考图的数据万象规则：长边 1536、JPEG 85。
	EditCOSProcess = "imageMogr2/thumbnail/1536x/format/jpg/quality/85"
)

type imageBudget struct {
	maxEdge      int
	jpegQuality  int
	keepMaxBytes int
}

var (
	visionBudget = imageBudget{maxEdge: visionMaxEdge, jpegQuality: visionJPEGQuality, keepMaxBytes: visionKeepMaxBytes}
	editBudget   = imageBudget{maxEdge: editMaxEdge, jpegQuality: editJPEGQuality, keepMaxBytes: editKeepMaxBytes}
)

// ConstrainVisionImage 收缩将要 base64 发给视觉模型的照片。非图片与
// 已经够小的字节原样透传，demo 夹具与 CI 处理结果不受影响。
func ConstrainVisionImage(data []byte, mimeType string) ([]byte, string) {
	return constrainImage(data, mimeType, visionBudget)
}

// ConstrainEditImage 为保身份编辑（方案 look / 渲染参考）保留更多面部
// 细节。透传规则与 ConstrainVisionImage 相同。
func ConstrainEditImage(data []byte, mimeType string) ([]byte, string) {
	return constrainImage(data, mimeType, editBudget)
}

func constrainImage(data []byte, mimeType string, budget imageBudget) ([]byte, string) {
	if len(data) == 0 || len(data) <= budget.keepMaxBytes {
		return data, mimeType
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, mimeType
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 {
		return data, mimeType
	}
	scale := 1.0
	if longest := width; height > longest {
		if height > budget.maxEdge {
			scale = float64(budget.maxEdge) / float64(height)
		}
	} else if width > budget.maxEdge {
		scale = float64(budget.maxEdge) / float64(width)
	}
	if scale >= 1 && len(data) <= budget.keepMaxBytes {
		return data, mimeType
	}
	outW, outH := width, height
	if scale < 1 {
		outW = int(float64(width) * scale)
		outH = int(float64(height) * scale)
		if outW < 1 {
			outW = 1
		}
		if outH < 1 {
			outH = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < outH; y++ {
		srcY := bounds.Min.Y + y*height/outH
		for x := 0; x < outW; x++ {
			srcX := bounds.Min.X + x*width/outW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: budget.jpegQuality}); err != nil {
		return data, mimeType
	}
	if buf.Len() == 0 || buf.Len() >= len(data) {
		return data, mimeType
	}
	return buf.Bytes(), "image/jpeg"
}
