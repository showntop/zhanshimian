package assessment

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestTechnicalPhotoChecker(t *testing.T) {
	validJPEG := encodedImage(t, "jpeg", 1200, 1600)
	validPNG := encodedImage(t, "png", 1200, 1600)
	tests := []struct {
		name, declaredMIME, code string
		data                     []byte
	}{
		{"jpeg", "image/jpeg", "", validJPEG},
		{"png", "image/png", "", validPNG},
		{"mime mismatch", "image/png", "photo_mime_mismatch", validJPEG},
		{"webp", "image/webp", "photo_format_unsupported", []byte("RIFFxxxxWEBP")},
		{"truncated", "image/jpeg", "photo_decode_failed", validJPEG[:64]},
		{"too many pixels", "image/png", "photo_dimensions_exceeded", pngHeader(10000, 10000)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := (TechnicalPhotoChecker{MaxBytes: 20 << 20, MaxDimension: 8192, MaxPixels: 40_000_000}).CheckOne(
				ImageInput{Role: "face", MIMEType: tc.declaredMIME, Data: tc.data},
			)
			assertPhotoCode(t, err, tc.code)
		})
	}
}

func TestTechnicalPhotoCheckerRejectsEmptyAndOversized(t *testing.T) {
	checker := TechnicalPhotoChecker{MaxBytes: 64, MaxDimension: 8192, MaxPixels: 40_000_000}
	assertPhotoCode(t, checker.CheckOne(ImageInput{Role: "face", MIMEType: "image/jpeg"}), "photo_size_invalid")
	assertPhotoCode(t, checker.CheckOne(ImageInput{Role: "side", MIMEType: "image/jpeg", Data: bytes.Repeat([]byte{0}, 65)}), "photo_size_invalid")
}

func TestPhotoRejectedErrorDoesNotLeakInternals(t *testing.T) {
	err := (TechnicalPhotoChecker{MaxBytes: 20 << 20, MaxDimension: 32, MaxPixels: 100}).CheckOne(
		ImageInput{Role: "body", MIMEType: "image/png", Data: encodedImage(t, "png", 64, 64)},
	)
	assertPhotoCode(t, err, "photo_dimensions_exceeded")
	msg := err.Error()
	if msg != photoPublicMessage["photo_dimensions_exceeded"] {
		t.Fatalf("Error() = %q", msg)
	}
	for _, leak := range []string{"64", "4096", "0x", "/", "\\", "IHDR", "PNG"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("Error() leaked %q: %q", leak, msg)
		}
	}
}

func TestTechnicalPhotoCheckerCheckStopsOnFirstRejection(t *testing.T) {
	validJPEG := encodedImage(t, "jpeg", 32, 32)
	err := (TechnicalPhotoChecker{MaxBytes: 20 << 20, MaxDimension: 8192, MaxPixels: 40_000_000}).Check([]ImageInput{
		{Role: "face", MIMEType: "image/jpeg", Data: validJPEG},
		{Role: "side", MIMEType: "image/webp", Data: []byte("RIFFxxxxWEBP")},
		{Role: "body", MIMEType: "image/jpeg", Data: validJPEG},
	})
	assertPhotoCode(t, err, "photo_format_unsupported")
	var rejected *PhotoRejectedError
	if !errors.As(err, &rejected) || rejected.Role != "side" {
		t.Fatalf("got %#v, want side", err)
	}
}

func assertPhotoCode(t *testing.T, err error, code string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	var rejected *PhotoRejectedError
	if !errors.As(err, &rejected) || rejected.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
	if msg := rejected.Error(); msg != photoPublicMessage[code] {
		t.Fatalf("Error() = %q, want public message for %s", msg, code)
	}
}

func encodedImage(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			t.Fatalf("encode jpeg: %v", err)
		}
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			t.Fatalf("encode png: %v", err)
		}
	default:
		t.Fatalf("unknown format %s", format)
	}
	return buf.Bytes()
}

func pngHeader(width, height int) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8] = 8
	ihdr[9] = 2
	chunkType := []byte("IHDR")
	digest := crc32.NewIEEE()
	_, _ = digest.Write(chunkType)
	_, _ = digest.Write(ihdr)
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(ihdr)))
	buf.Write(chunkType)
	buf.Write(ihdr)
	_ = binary.Write(&buf, binary.BigEndian, digest.Sum32())
	return buf.Bytes()
}
