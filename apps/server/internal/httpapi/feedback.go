package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/feedback"
)

// feedbackService is the narrow Feedback surface the transport may call.
type feedbackService interface {
	CreateGenerationFeedback(ctx context.Context, userID string, input feedback.CreateGenerationFeedbackInput) (feedback.GenerationFeedback, bool, error)
	CreateExecutionFeedback(ctx context.Context, userID string, input feedback.CreateExecutionFeedbackInput) (feedback.ExecutionFeedback, bool, error)
}

var errFeedbackUnavailable = errors.New("feedback service unavailable")

// createGenerationFeedbackRequest 只接受生成侧的四个字段;DisallowUnknownFields
// 让 preference 之类执行侧字段在传输层就被拒绝,而不是被静默忽略。
type createGenerationFeedbackRequest struct {
	PublicationID string       `json:"publication_id"`
	Tags          []domain.Tag `json:"tags"`
	Comment       string       `json:"comment"`
	MediaAssetID  *string      `json:"media_asset_id"`
}

// createExecutionFeedbackRequest 只接受执行侧的五个字段;preference 是唯一能形成
// 偏好记忆的结构化输入,自由文本永不进入。
type createExecutionFeedbackRequest struct {
	ExecutionID  string                      `json:"execution_id"`
	Tags         []domain.Tag                `json:"tags"`
	Comment      string                      `json:"comment"`
	MediaAssetID *string                     `json:"media_asset_id"`
	Preference   domain.StructuredPreference `json:"preference"`
}

// createGenerationFeedback 保存一条已发布 Generation 的反馈。反馈图片不内嵌上传:
// 客户端先走既有 upload-intent → complete,上传失败就省略 media_asset_id。
func (a *API) createGenerationFeedback(w http.ResponseWriter, r *http.Request) {
	if a.feedback == nil {
		a.internalError(w, r, errFeedbackUnavailable)
		return
	}
	var input createGenerationFeedbackRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
		return
	}
	result, created, err := a.feedback.CreateGenerationFeedback(r.Context(), currentUser(r).ID, feedback.CreateGenerationFeedbackInput{
		PublicationID:  input.PublicationID,
		Tags:           input.Tags,
		Comment:        input.Comment,
		MediaAssetID:   input.MediaAssetID,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		a.writeFeedbackError(w, r, err)
		return
	}
	writeData(w, createdStatus(created), result)
}

// createExecutionFeedback 保存一条已完成执行的反馈,并原样返回实际写入的
// applied_memories——客户端据此展示的确认文案才不会承诺未落库的记忆。
func (a *API) createExecutionFeedback(w http.ResponseWriter, r *http.Request) {
	if a.feedback == nil {
		a.internalError(w, r, errFeedbackUnavailable)
		return
	}
	var input createExecutionFeedbackRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
		return
	}
	result, created, err := a.feedback.CreateExecutionFeedback(r.Context(), currentUser(r).ID, feedback.CreateExecutionFeedbackInput{
		ExecutionID:    input.ExecutionID,
		Tags:           input.Tags,
		Comment:        input.Comment,
		MediaAssetID:   input.MediaAssetID,
		Preference:     input.Preference,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		a.writeFeedbackError(w, r, err)
		return
	}
	writeData(w, createdStatus(created), result)
}

func (a *API) writeFeedbackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, feedback.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error",
			strings.TrimPrefix(err.Error(), feedback.ErrValidation.Error()+": "))
	case errors.Is(err, domain.ErrPreferenceNotAllowed):
		// 偏好记忆只从合法的结构化输入派生;Generation 标签进入执行反馈即请求不合法。
		writeError(w, r, http.StatusBadRequest, "validation_error", "该偏好无法形成记忆")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
	case errors.Is(err, feedback.ErrExecutionNotCompleted):
		writeError(w, r, http.StatusConflict, "execution_not_completed", "请先完成全部步骤再提交反馈")
	case errors.Is(err, feedback.ErrIdempotencyConflict):
		writeError(w, r, http.StatusConflict, "idempotency_conflict", "相同幂等键已被用于不同请求")
	default:
		a.internalError(w, r, err)
	}
}
