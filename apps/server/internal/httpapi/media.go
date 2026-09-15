package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/media"
)

type createUploadIntentRequest struct {
	Purpose  string `json:"purpose"`
	MIMEType string `json:"mime_type"`
	ByteSize int64  `json:"byte_size"`
	SHA256   string `json:"sha256"`
}

type uploadIntentDTO struct {
	ID      string         `json:"id"`
	Purpose string         `json:"purpose"`
	Status  string         `json:"status"`
	Upload  uploadGrantDTO `json:"upload"`
}

type uploadGrantDTO struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt string            `json:"expires_at"`
}

type mediaAssetDTO struct {
	ID        string `json:"id"`
	Purpose   string `json:"purpose"`
	MIMEType  string `json:"mime_type"`
	ByteSize  int64  `json:"byte_size"`
	SHA256    string `json:"sha256"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

// 预签名 URL 过期后重放同一响应必然 403：重放前校验 expires_at 还有余量。
// 解析不出时间按新鲜处理，不误伤这条路由之外的重放语义。
func uploadGrantStillFresh(_ int, body []byte) bool {
	var payload struct {
		Data struct {
			Upload struct {
				ExpiresAt string `json:"expires_at"`
			} `json:"upload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return true
	}
	expires, err := time.Parse("2006-01-02T15:04:05Z", payload.Data.Upload.ExpiresAt)
	if err != nil {
		return true
	}
	return time.Until(expires) > time.Minute
}

func (a *API) createUploadIntent(w http.ResponseWriter, r *http.Request) {
	if a.media == nil {
		a.internalError(w, r, errMediaUnavailable)
		return
	}
	var input createUploadIntentRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	created, err := a.media.CreateUploadIntent(r.Context(), currentUser(r).ID, media.CreateIntentInput{
		Purpose:  domain.MediaPurpose(input.Purpose),
		MIMEType: input.MIMEType,
		ByteSize: input.ByteSize,
		SHA256:   input.SHA256,
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, uploadIntentDTO{
		ID:      created.ID,
		Purpose: string(created.Purpose),
		Status:  string(created.Status),
		Upload: uploadGrantDTO{
			Method:    created.Grant.Method,
			URL:       created.Grant.URL,
			Headers:   created.Grant.Headers,
			ExpiresAt: created.Grant.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		},
	})
}

func (a *API) completeUploadIntent(w http.ResponseWriter, r *http.Request) {
	if a.media == nil {
		a.internalError(w, r, errMediaUnavailable)
		return
	}
	if r.Body != nil && r.ContentLength != 0 {
		var body struct{}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
	}
	asset, created, err := a.media.CompleteVerifiedUpload(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeData(w, status, publicMediaAsset(asset))
}

func (a *API) createDemoMedia(w http.ResponseWriter, r *http.Request) {
	if a.demo == nil {
		a.internalError(w, r, errDemoUnavailable)
		return
	}
	var input struct {
		Role string `json:"role"`
		Kind string `json:"kind"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	kind := strings.TrimSpace(input.Role)
	if kind == "" {
		kind = input.Kind
	}
	asset, err := a.demo.CreateDemoMedia(r.Context(), currentUser(r).ID, kind)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, publicMediaAsset(asset))
}

func publicMediaAsset(asset domain.MediaAsset) mediaAssetDTO {
	return mediaAssetDTO{
		ID:        asset.ID,
		Purpose:   string(asset.Purpose),
		MIMEType:  asset.MIMEType,
		ByteSize:  asset.ByteSize,
		SHA256:    asset.SHA256,
		State:     string(asset.State),
		CreatedAt: asset.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

var errDemoUnavailable = errors.New("demo media creator unavailable")
