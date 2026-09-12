package service

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestConstrainVisionImageLeavesSmallOrInvalidBytes(t *testing.T) {
	raw := []byte("face-image")
	got, mime := constrainVisionImage(raw, "image/jpeg")
	if string(got) != "face-image" || mime != "image/jpeg" {
		t.Fatalf("non-image bytes should pass through, got %q %s", got, mime)
	}
}

func TestConstrainVisionImageDownscalesLargeJPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2400, 1600))
	for y := 0; y < 1600; y++ {
		for x := 0; x < 2400; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x * y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	original := buf.Bytes()
	if len(original) <= visionKeepMaxBytes {
		t.Fatalf("fixture too small to exercise downscale: %d", len(original))
	}
	got, mime := constrainVisionImage(original, "image/jpeg")
	if mime != "image/jpeg" {
		t.Fatalf("unexpected mime %s", mime)
	}
	if len(got) >= len(original) {
		t.Fatalf("expected smaller payload, original=%d got=%d", len(original), len(got))
	}
	decoded, _, err := image.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	b := decoded.Bounds()
	if b.Dx() > visionMaxEdge || b.Dy() > visionMaxEdge {
		t.Fatalf("downscaled image still too large: %dx%d", b.Dx(), b.Dy())
	}
}
