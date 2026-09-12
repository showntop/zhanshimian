package httpapi

import (
	"net/http"
	"testing"
)

func TestReportResponseOmitsInternalFieldsAndIncludesEvidencePhoto(t *testing.T) {
	res := newReportAPI(t).Do(http.MethodGet, "/v1/reports/report-1", "", nil)
	assertStatus(t, res, 200)
	assertJSONPath(t, res, "data.findings.0.source_photo.role", "face")
	assertJSONPath(t, res, "data.findings.0.anchor.w", .2)
	for _, field := range []string{"confidence", "provider_invocation_id", "quality_evaluation_id", "provider_version"} {
		assertJSONDoesNotContainKey(t, res, field)
	}
}

func TestReportCrossTenantIs404(t *testing.T) {
	res := newReportAPIAs(t, "other-user").Do(http.MethodGet, "/v1/reports/report-1", "", nil)
	assertError(t, res, 404, "not_found", false)
}
