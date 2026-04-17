package transcoder

import (
	"fmt"
	"time"
)

// TranscodeResult 转码结果（mock：假设已预转码）
type TranscodeResult struct {
	AdID        string
	Profile     CodecProfile
	SegmentURIs []string
	SegDuration float64
}

// MockTranscoder 开发用 mock 转码器（不真正转码，生成预置 URL）
type MockTranscoder struct {
	BaseURL string
}

// NewMockTranscoder 创建 mock 转码器
func NewMockTranscoder(baseURL string) *MockTranscoder {
	return &MockTranscoder{BaseURL: baseURL}
}

// Transcode 模拟转码，返回广告 segment URI 列表
// durationSec 是广告时长，segDuration 是每个分片时长
func (t *MockTranscoder) Transcode(adID string, vastMediaURL string, durationSec, segDuration float64) (*TranscodeResult, error) {
	if adID == "" {
		return nil, fmt.Errorf("transcode: adID is required")
	}
	if durationSec <= 0 {
		return nil, fmt.Errorf("transcode: durationSec must be positive")
	}
	if segDuration <= 0 {
		segDuration = 10
	}

	// 假设广告已按 360p 预转码
	profile := MatchProfile(854, 480, 1500)
	n := int(durationSec/segDuration) + 1
	uris := make([]string, n)
	for i := 0; i < n; i++ {
		uris[i] = fmt.Sprintf("%s/prerolls/%s/%s/seg_%03d.ts", t.BaseURL, adID, profile.Name, i+1)
	}

	return &TranscodeResult{
		AdID:        adID,
		Profile:     profile,
		SegmentURIs: uris,
		SegDuration: segDuration,
	}, nil
}

// TranscodeAsync 异步转码（生产环境走消息队列，这里直接同步返回）
func (t *MockTranscoder) TranscodeAsync(adID string, vastMediaURL string, durationSec, segDuration float64, callback func(*TranscodeResult, error)) {
	go func() {
		// 模拟短暂延迟
		time.Sleep(5 * time.Millisecond)
		result, err := t.Transcode(adID, vastMediaURL, durationSec, segDuration)
		callback(result, err)
	}()
}
