package realtimehttp

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
)

func RegisterWatches(mux *http.ServeMux, auth *identityhttp.Handler, routing *app.Routing) {
	mux.Handle("POST /api/v1/realtime/availability-watches", auth.RequireSameOrigin(auth.RequireAuthentication(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		fail := func(status int) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"Availability watch refused"}`))
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			fail(400)
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			fail(400)
			return
		}
		var input struct {
			TutorID string `json:"tutor_id"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			fail(400)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			fail(400)
			return
		}
		p, _ := identityhttp.CurrentPrincipal(r.Context())
		end, err := routing.Watch(r.Context(), p, input.TutorID)
		if errors.Is(err, app.ErrWatch) {
			fail(403)
			return
		}
		if err != nil {
			fail(503)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			ExpiresAt string `json:"expires_at"`
		}{end.UTC().Format(time.RFC3339Nano)})
	}))))
}
