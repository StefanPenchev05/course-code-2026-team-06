package coordinationhttp

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

type AuditHandler struct {
	service *application.AuditService
	errors  *IssueHandler
}

func NewAudit(service *application.AuditService, logger *slog.Logger) *AuditHandler {
	return &AuditHandler{service: service, errors: &IssueHandler{logger: logger}}
}
func (h *AuditHandler) RegisterRoutes(mux *http.ServeMux, auth *identityhttp.Handler) {
	mux.Handle("GET /api/v1/coordinator/users/{id}/audit-history", auth.RequireAuthentication(auth.RequireCoordinator(http.HandlerFunc(h.list))))
}
func auditFilters(raw string) (pagination, string, error) {
	p, category := pagination{Limit: 25}, "SUSPENSION"
	params, err := url.ParseQuery(raw)
	if err != nil {
		return p, category, errMalformed
	}
	for k, values := range params {
		if len(values) != 1 {
			return p, category, errMalformed
		}
		switch k {
		case "category":
			category = values[0]
		case "limit", "offset":
			v, err := strconv.Atoi(values[0])
			if err != nil {
				return p, category, errMalformed
			}
			if k == "limit" {
				p.Limit = v
			} else {
				p.Offset = v
			}
		default:
			return p, category, errMalformed
		}
	}
	return p, category, nil
}
func (h *AuditHandler) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, category, err := auditFilters(r.URL.RawQuery)
	if err != nil {
		h.errors.fail(w, r, err)
		return
	}
	actor, _ := identityhttp.CurrentPrincipal(r.Context())
	events, err := h.service.List(r.Context(), actor, r.PathValue("id"), category, p.Limit, p.Offset)
	if err != nil {
		h.errors.fail(w, r, err)
		return
	}
	p.HasMore = len(events) > p.Limit
	if p.HasMore {
		events = events[:p.Limit]
	}
	write(w, 200, struct {
		Data       []domain.AuditEntry `json:"data"`
		Pagination pagination          `json:"pagination"`
	}{events, p})
}
