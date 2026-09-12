package service

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
	// 数据万象：长边 1280、JPEG 75。未开通 CI 时 OpenProcessed 失败，走本地缩放。
	visionCOSProcess = "imageMogr2/thumbnail/1280x/format/jpg/quality/75"
)

// constrainVisionImage shrinks a still that will be base64-posted to a vision
// model. Non-images and already-small files are returned unchanged so tests
// and demo fixtures keep working.
func constrainVisionImage(data []byte, mimeType string) ([]byte, string) {
	if len(data) == 0 || len(data) <= visionKeepMaxBytes {
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
		if height > visionMaxEdge {
			scale = float64(visionMaxEdge) / float64(height)
		}
	} else if width > visionMaxEdge {
		scale = float64(visionMaxEdge) / float64(width)
	}
	if scale >= 1 && len(data) <= visionKeepMaxBytes {
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
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: visionJPEGQuality}); err != nil {
		return data, mimeType
	}
	if buf.Len() == 0 || buf.Len() >= len(data) {
		return data, mimeType
	}
	return buf.Bytes(), "image/jpeg"
}
