package assessment

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
)

type ImageInput struct {
	Role     string
	MIMEType string
	Data     []byte
}

type TechnicalPhotoChecker struct {
	MaxBytes     int64
	MaxDimension int
	MaxPixels    int64
}

type PhotoRejectedError struct {
	Role string
	Code string
}

func (e *PhotoRejectedError) Error() string {
	if e == nil {
		return ""
	}
	if msg, ok := photoPublicMessage[e.Code]; ok {
		return msg
	}
	return photoPublicMessage["photo_decode_failed"]
}

var photoPublicMessage = map[string]string{
	"photo_size_invalid":        "照片文件过大或为空，请重新选择一张照片",
	"photo_format_unsupported":  "仅支持 JPEG 或 PNG，请更换照片后重试",
	"photo_mime_mismatch":       "照片格式无法确认，请重新选择原始照片",
	"photo_decode_failed":       "照片文件无法读取，请重新拍摄或更换照片",
	"photo_dimensions_exceeded": "照片尺寸过大，请压缩后重新上传",
}

func (c TechnicalPhotoChecker) Check(inputs []ImageInput) error {
	for _, input := range inputs {
		if err := c.CheckOne(input); err != nil {
			return err
		}
	}
	return nil
}

func (c TechnicalPhotoChecker) CheckOne(input ImageInput) error {
	if len(input.Data) == 0 || int64(len(input.Data)) > c.MaxBytes {
		return reject(input.Role, "photo_size_invalid")
	}
	detected := http.DetectContentType(input.Data)
	if detected != "image/jpeg" && detected != "image/png" {
		return reject(input.Role, "photo_format_unsupported")
	}
	if detected != input.MIMEType {
		return reject(input.Role, "photo_mime_mismatch")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(input.Data))
	if err != nil || "image/"+format != detected {
		return reject(input.Role, "photo_decode_failed")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > c.MaxDimension || cfg.Height > c.MaxDimension ||
		int64(cfg.Width)*int64(cfg.Height) > c.MaxPixels {
		return reject(input.Role, "photo_dimensions_exceeded")
	}
	if _, _, err := image.Decode(bytes.NewReader(input.Data)); err != nil {
		return reject(input.Role, "photo_decode_failed")
	}
	return nil
}

func reject(role, code string) error {
	return &PhotoRejectedError{Role: role, Code: code}
}
