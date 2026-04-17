package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/adortb/adortb-ssai/internal/decision"
	"github.com/adortb/adortb-ssai/internal/manifest"
	"github.com/adortb/adortb-ssai/internal/slate"
	"github.com/adortb/adortb-ssai/internal/tracking"
	"github.com/adortb/adortb-ssai/internal/transcoder"
)

// Config API 服务配置
type Config struct {
	SelfBaseURL    string // 本服务对外地址，用于改写 segment URL
	AdxBaseURL     string
	SlateBaseURL   string
	SegDurationSec float64
}

// Handler SSAI HTTP 处理器
type Handler struct {
	cfg         Config
	sessions    *tracking.SessionStore
	beacon      *tracking.BeaconProxy
	decClient   *decision.Client
	slateGen    *slate.Generator
	transcoder  *transcoder.MockTranscoder
	httpFetcher *http.Client
}

// NewHandler 创建 API 处理器
func NewHandler(cfg Config, sessions *tracking.SessionStore) *Handler {
	if cfg.SegDurationSec <= 0 {
		cfg.SegDurationSec = 10
	}
	return &Handler{
		cfg:      cfg,
		sessions: sessions,
		beacon:   tracking.NewBeaconProxy(50),
		decClient: decision.NewClient(cfg.AdxBaseURL),
		slateGen: slate.NewGenerator(slate.SlateConfig{
			BaseURL:     cfg.SlateBaseURL,
			SegDuration: cfg.SegDurationSec,
		}),
		transcoder: transcoder.NewMockTranscoder(cfg.SelfBaseURL),
		httpFetcher: &http.Client{Timeout: 5 * time.Second},
	}
}

// MasterManifest GET /v1/session/:session_id/master.m3u8?content=<base64_url>
func (h *Handler) MasterManifest(w http.ResponseWriter, r *http.Request) {
	sessionID := pathParam(r.URL.Path, "/v1/session/", "/master.m3u8")
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	contentParam := r.URL.Query().Get("content")
	if contentParam == "" {
		http.Error(w, "missing content param", http.StatusBadRequest)
		return
	}
	contentURLBytes, err := base64.URLEncoding.DecodeString(contentParam)
	if err != nil {
		http.Error(w, "invalid content param", http.StatusBadRequest)
		return
	}
	contentURL := string(contentURLBytes)

	// 获取或创建播放会话
	_, err = h.sessions.GetOrCreate(r.Context(), sessionID, contentURL)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	// 拉取原始内容 manifest
	hlsContent, err := h.fetchManifest(r.Context(), contentURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetch manifest: %v", err), http.StatusBadGateway)
		return
	}

	// 解析 HLS
	m, err := manifest.ParseHLS(hlsContent)
	if err != nil {
		http.Error(w, fmt.Sprintf("parse manifest: %v", err), http.StatusBadGateway)
		return
	}

	// 向 adx 请求广告决策
	breakPositions := cuePointPositions(m.CuePoints)
	breakDur := 30.0
	if len(m.CuePoints) > 0 {
		breakDur = m.CuePoints[0].Duration
	}

	adResp, err := h.decClient.Decide(r.Context(), decision.AdDecision{
		ContentDuration: totalDuration(m.Segments),
		BreakPositions:  breakPositions,
		BreakDuration:   breakDur,
		User: decision.UserContext{
			IP:        r.RemoteAddr,
			UserAgent: r.UserAgent(),
		},
	})

	// 构建广告 breaks（adx 失败时用 slate 兜底）
	adBreaks := h.buildAdBreaks(r.Context(), m, adResp)

	// 缝合 manifest
	stitched := manifest.StitchHLS(m, manifest.StitchOptions{
		SessionID: sessionID,
		BaseURL:   h.cfg.SelfBaseURL,
		AdBreaks:  adBreaks,
	})

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, stitched)
}

// AdSegmentProxy GET /v1/session/:session_id/ad_:ad_id/:filename
// 代理广告 ts 分片，同时触发跟踪事件
func (h *Handler) AdSegmentProxy(w http.ResponseWriter, r *http.Request) {
	// 解析 /v1/session/{sid}/ad_{adid}/{filename}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/session/"), "/")
	if len(parts) < 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]
	adPart := parts[1]    // ad_xxxx
	filename := parts[2]

	if !strings.HasPrefix(adPart, "ad_") {
		http.Error(w, "invalid ad segment path", http.StatusBadRequest)
		return
	}
	adID := strings.TrimPrefix(adPart, "ad_")

	// 重建原始广告 CDN URL
	origURL := r.URL.Query().Get("src")
	if origURL == "" {
		// 从 mock transcoder URL 构建
		origURL = fmt.Sprintf("%s/prerolls/%s/480p/%s", h.cfg.AdxBaseURL, adID, filename)
	}

	// 记录 segment 播放（异步）
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		totalSegs := 3 // 默认 3 segments（生产环境从 session 元数据读取）
		evts, err := h.sessions.RecordSegmentPlayed(ctx, sessionID, adID, totalSegs)
		if err != nil {
			return
		}
		// 代发 beacon（实际 URL 从 VAST tracking events 获取）
		if len(evts) > 0 {
			beaconURLs := mockBeaconURLs(sessionID, adID, evts)
			h.beacon.Fire(ctx, beaconURLs)
		}
	}()

	// 代理实际 ts 内容
	h.proxySegment(w, r, origURL)
}

// TrackingBeacon POST /v1/tracking/beacon
func (h *Handler) TrackingBeacon(w http.ResponseWriter, r *http.Request) {
	var req tracking.BeaconRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	h.beacon.Fire(r.Context(), req.URLs)
	w.WriteHeader(http.StatusNoContent)
}

// SessionEvents GET /v1/session/:session_id/events
func (h *Handler) SessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := pathParam(r.URL.Path, "/v1/session/", "/events")
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	events, err := h.sessions.GetEvents(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sessionID,
		"events":     events,
		"count":      len(events),
	})
}

// DirectDecision POST /v1/decision
func (h *Handler) DirectDecision(w http.ResponseWriter, r *http.Request) {
	var req decision.AdDecision
	if err := decodeJSON(r.Body, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
	defer cancel()

	resp, err := h.decClient.Decide(ctx, req)
	if err != nil {
		// 返回空决策
		resp = &decision.DecisionResponse{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// Health GET /health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- 内部辅助 ----

func (h *Handler) fetchManifest(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := h.httpFetcher.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *Handler) proxySegment(w http.ResponseWriter, r *http.Request, origURL string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, origURL, nil)
	if err != nil {
		http.Error(w, "bad origin url", http.StatusBadGateway)
		return
	}
	resp, err := h.httpFetcher.Do(req)
	if err != nil {
		http.Error(w, "fetch segment failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "video/mp2t")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *Handler) buildAdBreaks(ctx context.Context, m *manifest.HLSManifest, adResp *decision.DecisionResponse) []manifest.AdBreak {
	if adResp == nil || len(adResp.Breaks) == 0 {
		// 无广告：在 cue point 位置插入 slate
		return h.slateBreaks(m)
	}

	breaks := make([]manifest.AdBreak, 0, len(adResp.Breaks))
	for i, br := range adResp.Breaks {
		if len(br.Ads) == 0 {
			// 该 break 无广告，使用 slate
			pos := cuePointPosition(m, i)
			slateURIs := h.slateGen.SlateSegments(br.Ads[0].DurationSec)
			breaks = append(breaks, manifest.BuildAdBreak(pos, "slate", slateURIs, h.cfg.SegDurationSec))
			continue
		}
		ad := br.Ads[0] // 取第一个广告（简单实现）
		result, err := h.transcoder.Transcode(ad.AdID, ad.MediaURL, ad.DurationSec, h.cfg.SegDurationSec)
		if err != nil {
			// 转码失败，slate 兜底
			pos := cuePointPosition(m, i)
			slateURIs := h.slateGen.SlateSegments(ad.DurationSec)
			breaks = append(breaks, manifest.BuildAdBreak(pos, "slate", slateURIs, h.cfg.SegDurationSec))
			continue
		}
		pos := cuePointPosition(m, i)
		breaks = append(breaks, manifest.BuildAdBreak(pos, ad.AdID, result.SegmentURIs, result.SegDuration))
	}
	return breaks
}

func (h *Handler) slateBreaks(m *manifest.HLSManifest) []manifest.AdBreak {
	breaks := make([]manifest.AdBreak, 0, len(m.CuePoints))
	for i, cp := range m.CuePoints {
		slateURIs := h.slateGen.SlateSegments(cp.Duration)
		breaks = append(breaks, manifest.BuildAdBreak(cp.Position, fmt.Sprintf("slate_%d", i), slateURIs, h.cfg.SegDurationSec))
	}
	return breaks
}

func cuePointPositions(cps []manifest.CuePoint) []float64 {
	out := make([]float64, len(cps))
	for i, cp := range cps {
		out[i] = float64(cp.Position) * 10 // 粗略估算（每 segment 10s）
	}
	return out
}

func cuePointPosition(m *manifest.HLSManifest, idx int) int {
	if idx < len(m.CuePoints) {
		return m.CuePoints[idx].Position
	}
	return len(m.Segments)
}

func totalDuration(segs []manifest.Segment) float64 {
	var total float64
	for _, s := range segs {
		total += s.Duration
	}
	return total
}

func mockBeaconURLs(sessionID, adID string, evts []tracking.EventType) []string {
	urls := make([]string, 0, len(evts))
	for _, e := range evts {
		urls = append(urls, fmt.Sprintf("https://track.adortb.com/beacon?sess=%s&ad=%s&event=%s", sessionID, adID, e))
	}
	return urls
}
