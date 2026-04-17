package api

import (
	"net/http"
)

// NewRouter 构建并返回 HTTP 路由
func NewRouter(h *Handler) http.Handler {
	mux := http.NewServeMux()

	// 主 manifest 路由（带 session）
	mux.HandleFunc("/v1/session/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case hasSuffix(path, "/master.m3u8") && r.Method == http.MethodGet:
			h.MasterManifest(w, r)
		case hasSuffix(path, "/events") && r.Method == http.MethodGet:
			h.SessionEvents(w, r)
		case containsSegment(path, "/ad_") && r.Method == http.MethodGet:
			h.AdSegmentProxy(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("POST /v1/tracking/beacon", h.TrackingBeacon)
	mux.HandleFunc("POST /v1/decision", h.DirectDecision)
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /metrics", h.Metrics)

	return mux
}

// Metrics GET /metrics（Prometheus 格式骨架）
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("# HELP ssai_requests_total Total SSAI manifest requests\n" +
		"# TYPE ssai_requests_total counter\n" +
		"ssai_requests_total 0\n"))
}

func hasSuffix(path, suffix string) bool {
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}

func containsSegment(path, sub string) bool {
	for i := 0; i <= len(path)-len(sub); i++ {
		if path[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
