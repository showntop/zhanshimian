package rendering

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func jpegFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	return encodeFixture(t, width, height, "jpeg")
}

func pngFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	return encodeFixture(t, width, height, "png")
}

func encodeFixture(t *testing.T, width, height int, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x += 32 {
		for y := 0; y < height; y += 32 {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 96, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// minimalVP8 是公知的 1x1 VP8L (lossless) WebP,base64 解码即得。
func minimalVP8() []byte {
	const encoded = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		panic(err)
	}
	return data
}

func TestNormalizeAlwaysReturnsDecodableJPEGWithoutMetadata(t *testing.T) {
	for _, fixture := range []struct {
		name string
		mime string
		data []byte
	}{
		{"jpeg", "image/jpeg", jpegFixture(t, 800, 1200)},
		{"png", "image/png", pngFixture(t, 800, 1200)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got, err := NewJPEGNormalizer().Normalize(fixture.data, fixture.mime)
			if err != nil {
				t.Fatal(err)
			}
			if got.MIMEType != "image/jpeg" ||
				!bytes.HasPrefix(got.Data, []byte{0xff, 0xd8, 0xff}) {
				t.Fatalf("not JPEG: mime=%s prefix=%x", got.MIMEType, got.Data[:3])
			}
			if got.Width != 800 || got.Height != 1200 ||
				got.SHA256 == "" || got.ByteSize != int64(len(got.Data)) {
				t.Fatalf("metadata mismatch: %#v", got)
			}
		})
	}
}

func TestNormalizeWebPInput(t *testing.T) {
	// x/image/webp 只提供解码器,这里用手工构造的最小 VP8 lossy 文件。
	data := minimalVP8()
	got, err := NewJPEGNormalizer().Normalize(data, "image/webp")
	if err != nil {
		t.Fatal(err)
	}
	if got.MIMEType != "image/jpeg" || got.Width != got.Height {
		t.Fatalf("unexpected webp normalization: %#v", got)
	}
}

func TestNormalizeRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name     string
		mime     string
		data     []byte
		wantCode string
	}{
		{"empty", "image/jpeg", nil, codeImageEmpty},
		{"mime mismatch", "image/jpeg", pngFixture(t, 64, 64), codeMIMEMismatch},
		{"plain bytes", "image/jpeg", []byte("not an image at all"), codeImageUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewJPEGNormalizer().Normalize(tc.data, tc.mime)
			var rejection *RejectionError
			if err == nil || !asRejection(err, &rejection) || rejection.Code != tc.wantCode {
				t.Fatalf("got %v, want %s", err, tc.wantCode)
			}
		})
	}
}

func TestNormalizeRejectsOversizedPixelCount(t *testing.T) {
	// 构造 DecodeConfig 层 40000001 像素的 PNG 头(稀疏数据,不完整解码)。
	huge := pngHeader(5001, 8000)
	normalizer := NewJPEGNormalizer()
	_, err := normalizer.Normalize(huge, "image/png")
	var rejection *RejectionError
	if err == nil || !asRejection(err, &rejection) || rejection.Code != codeImageTooLarge {
		t.Fatalf("got %v, want image_too_large", err)
	}
}

func pngHeader(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func asRejection(err error, target **RejectionError) bool {
	if r, ok := err.(*RejectionError); ok {
		*target = r
		return true
	}
	return false
}
