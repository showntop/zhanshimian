package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/billing"
)

type createAssessmentRequest struct {
	Photos domain.PhotoSlots `json:"photos"`
}

type assessmentResponse struct {
	ID         string `json:"id"`
	PhotoSetID string `json:"photo_set_id"`
	State      string `json:"state"`
	CreatedAt  string `json:"created_at"`
}

func (a *API) createAssessment(w http.ResponseWriter, r *http.Request) {
	if a.assessment == nil {
		a.internalError(w, r, errAssessmentUnavailable)
		return
	}
	var input createAssessmentRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := a.assessment.Create(r.Context(), assessment.CreateCommand{
		UserID: currentUser(r).ID,
		Slots:  input.Photos,
	})
	if err != nil {
		a.writeAssessmentError(w, r, err)
		return
	}
	state := string(result.Operation.Status)
	if state == "" {
		state = string(domain.OperationAccepted)
	}
	writeDataOperation(w, http.StatusAccepted, assessmentResponse{
		ID:         result.Run.ID,
		PhotoSetID: firstNonEmpty(result.Run.PhotoSetID, result.PhotoSet.ID),
		State:      state,
		CreatedAt:  formatPublicTime(result.Run.CreatedAt),
	}, publicOperation(result.Operation))
}

func (a *API) writeAssessmentError(w http.ResponseWriter, r *http.Request, err error) {
	var rejected *assessment.ValidationError
	if errors.As(err, &rejected) {
		writeError(w, r, http.StatusBadRequest, rejected.Code, "请检查拍摄的三张照片后重试")
		return
	}
	if errors.Is(err, billing.ErrRateLimited) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", strings.TrimPrefix(err.Error(), billing.ErrRateLimited.Error()+": "))
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
		return
	}
	a.internalError(w, r, err)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var errAssessmentUnavailable = errors.New("assessment service unavailable")
