package rendering

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"

	xwebp "golang.org/x/image/webp"
)

// NormalizedJPEG is clean, re-encoded JPEG without provider metadata.
type NormalizedJPEG struct {
	Data     []byte
	MIMEType string
	SHA256   string
	ByteSize int64
	Width    int
	Height   int
}

// DecoderConfig bounds the normalization input and output quality.
type DecoderConfig struct {
	MaxInputBytes int64
	MaxPixels     int64
	JPEGQuality   int
}

// Decoder normalizes provider JPEG/PNG/WebP bytes into clean JPEG.
type Decoder struct {
	config DecoderConfig
}

// NewJPEGNormalizer returns the production decoder bounds.
func NewJPEGNormalizer() *Decoder {
	return &Decoder{config: DecoderConfig{
		MaxInputBytes: 20 << 20,
		MaxPixels:     40_000_000,
		JPEGQuality:   92,
	}}
}

// Normalization failure codes surfaced through RejectionError.
const (
	codeImageEmpty               = "image_empty"
	codeImageTooLarge            = "image_too_large"
	codeMIMEMismatch             = "mime_mismatch"
	codeImageUnsupported         = "image_unsupported"
	codeImageDecodeFailed        = "image_decode_failed"
	codeAnimatedImageUnsupported = "animated_image_unsupported"
)

// RejectionError carries a stable normalization failure code.
type RejectionError struct {
	Code string
	Err  error
}

func (e *RejectionError) Error() string { return e.Code }
func (e *RejectionError) Unwrap() error { return e.Err }

func rejection(code string, err error) error {
	return &RejectionError{Code: code, Err: err}
}

// Normalize always returns a freshly encoded, decodable JPEG: re-encoding
// strips EXIF/XMP/ICC and any provider-side metadata.
func (d *Decoder) Normalize(data []byte, declaredMIME string) (NormalizedJPEG, error) {
	if len(data) == 0 {
		return NormalizedJPEG{}, rejection(codeImageEmpty, errors.New("empty image"))
	}
	if int64(len(data)) > d.config.MaxInputBytes {
		return NormalizedJPEG{}, rejection(codeImageTooLarge, fmt.Errorf("input %d bytes exceeds limit", len(data)))
	}
	detected := http.DetectContentType(data)
	switch detected {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return NormalizedJPEG{}, rejection(codeImageUnsupported, fmt.Errorf("unsupported content type %s", detected))
	}
	if declaredMIME != "" && detected != declaredMIME {
		return NormalizedJPEG{}, rejection(codeMIMEMismatch, fmt.Errorf("declared %s but detected %s", declaredMIME, detected))
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if detected == "image/webp" && isAnimatedWebP(data) {
			return NormalizedJPEG{}, rejection(codeAnimatedImageUnsupported, err)
		}
		return NormalizedJPEG{}, rejection(codeImageDecodeFailed, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 {
		return NormalizedJPEG{}, rejection(codeImageDecodeFailed, fmt.Errorf("invalid dimensions %dx%d", cfg.Width, cfg.Height))
	}
	if int64(cfg.Width)*int64(cfg.Height) > d.config.MaxPixels {
		return NormalizedJPEG{}, rejection(codeImageTooLarge, fmt.Errorf("%dx%d exceeds pixel limit", cfg.Width, cfg.Height))
	}

	decoded, err := decodeImage(detected, data)
	if err != nil {
		if detected == "image/webp" && isAnimatedWebP(data) {
			return NormalizedJPEG{}, rejection(codeAnimatedImageUnsupported, err)
		}
		return NormalizedJPEG{}, rejection(codeImageDecodeFailed, err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 {
		return NormalizedJPEG{}, rejection(codeImageDecodeFailed, errors.New("empty decoded bounds"))
	}

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, decoded, &jpeg.Options{Quality: d.config.JPEGQuality}); err != nil {
		return NormalizedJPEG{}, rejection(codeImageDecodeFailed, err)
	}
	encoded := buffer.Bytes()
	// 重新 DecodeConfig 验证输出可解码。
	if _, _, err := image.DecodeConfig(bytes.NewReader(encoded)); err != nil {
		return NormalizedJPEG{}, rejection(codeImageDecodeFailed, err)
	}
	sum := sha256.Sum256(encoded)
	return NormalizedJPEG{
		Data:     encoded,
		MIMEType: "image/jpeg",
		SHA256:   hex.EncodeToString(sum[:]),
		ByteSize: int64(len(encoded)),
		Width:    bounds.Dx(),
		Height:   bounds.Dy(),
	}, nil
}

func decodeImage(detected string, data []byte) (image.Image, error) {
	switch detected {
	case "image/jpeg":
		return jpeg.Decode(bytes.NewReader(data))
	case "image/png":
		return png.Decode(bytes.NewReader(data))
	case "image/webp":
		return xwebp.Decode(bytes.NewReader(data))
	}
	return nil, fmt.Errorf("unsupported format %s", detected)
}

// isAnimatedWebP checks the extended WebP header animation bit without a
// full decode.
func isAnimatedWebP(data []byte) bool {
	if len(data) < 21 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	return data[15] == 'L' && data[16] == 'E' && data[17] == 'F' && data[18] == 'S' &&
		len(data) > 20 && data[20]&0x02 != 0
}
