package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AdDecision 广告决策请求
type AdDecision struct {
	ContentDuration float64         `json:"content_duration"`
	BreakPositions  []float64       `json:"break_positions"`
	BreakDuration   float64         `json:"break_duration"`
	User            UserContext     `json:"user"`
}

// UserContext 用户上下文（传给 adx）
type UserContext struct {
	ID         string            `json:"id,omitempty"`
	IP         string            `json:"ip,omitempty"`
	UserAgent  string            `json:"user_agent,omitempty"`
	Geo        GeoContext        `json:"geo,omitempty"`
	Ext        map[string]string `json:"ext,omitempty"`
}

// GeoContext 地理位置
type GeoContext struct {
	Country string `json:"country,omitempty"`
	Region  string `json:"region,omitempty"`
	City    string `json:"city,omitempty"`
}

// AdBreakResponse 单个广告 break 的响应
type AdBreakResponse struct {
	PositionSec float64   `json:"position_sec"`
	Ads         []AdInfo  `json:"ads"`
}

// AdInfo 单条广告信息
type AdInfo struct {
	AdID         string  `json:"ad_id"`
	VastURL      string  `json:"vast_url"`
	DurationSec  float64 `json:"duration_sec"`
	MediaURL     string  `json:"media_url"`
	Price        float64 `json:"price"`
	ClickURL     string  `json:"click_url,omitempty"`
}

// DecisionResponse 广告决策响应
type DecisionResponse struct {
	Breaks []AdBreakResponse `json:"breaks"`
}

// Client 广告决策客户端（调用 adortb-adx）
type Client struct {
	adxBaseURL string
	httpClient *http.Client
}

// NewClient 创建决策客户端
func NewClient(adxBaseURL string) *Client {
	return &Client{
		adxBaseURL: adxBaseURL,
		httpClient: &http.Client{Timeout: 200 * time.Millisecond},
	}
}

// Decide 向 adortb-adx 发起竞价请求，返回广告决策
func (c *Client) Decide(ctx context.Context, req AdDecision) (*DecisionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("decision: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.adxBaseURL+"/v1/ssai/bid", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decision: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("decision: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("decision: adx returned %d", resp.StatusCode)
	}

	var dr DecisionResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return nil, fmt.Errorf("decision: decode: %w", err)
	}
	return &dr, nil
}
