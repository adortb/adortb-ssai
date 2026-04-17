package manifest

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

// Segment 表示 HLS 单个分片
type Segment struct {
	URI      string
	Duration float64
	IsAd     bool
	AdID     string
}

// CuePoint 广告插入点
type CuePoint struct {
	Duration float64 // 秒
	Position int     // 在 segments 中的位置
}

// HLSManifest 解析后的 HLS manifest
type HLSManifest struct {
	Version        int
	TargetDuration float64
	Segments       []Segment
	CuePoints      []CuePoint
	IsEndList      bool
}

// ParseHLS 解析 m3u8 文本为 HLSManifest
func ParseHLS(content string) (*HLSManifest, error) {
	m := &HLSManifest{Version: 3}
	scanner := bufio.NewScanner(strings.NewReader(content))

	var pendingDuration float64
	var inCue bool
	var cueDuration float64
	cueStartPos := -1

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case line == "#EXTM3U":
			// header

		case strings.HasPrefix(line, "#EXT-X-VERSION:"):
			fmt.Sscanf(strings.TrimPrefix(line, "#EXT-X-VERSION:"), "%d", &m.Version)

		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			fmt.Sscanf(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), "%f", &m.TargetDuration)

		case strings.HasPrefix(line, "#EXTINF:"):
			val := strings.TrimPrefix(line, "#EXTINF:")
			val = strings.TrimSuffix(val, ",")
			fmt.Sscanf(val, "%f", &pendingDuration)

		case strings.HasPrefix(line, "#EXT-X-CUE-OUT:"):
			fmt.Sscanf(strings.TrimPrefix(line, "#EXT-X-CUE-OUT:"), "%f", &cueDuration)
			inCue = true
			cueStartPos = len(m.Segments)

		case line == "#EXT-X-CUE-IN":
			if inCue && cueStartPos >= 0 {
				m.CuePoints = append(m.CuePoints, CuePoint{
					Duration: cueDuration,
					Position: cueStartPos,
				})
			}
			inCue = false
			cueStartPos = -1

		case line == "#EXT-X-ENDLIST":
			m.IsEndList = true

		case !strings.HasPrefix(line, "#"):
			seg := Segment{
				URI:      line,
				Duration: pendingDuration,
			}
			if inCue {
				// 忽略 cue 区间内的原始内容 segment（会被广告替换）
			} else {
				m.Segments = append(m.Segments, seg)
			}
			pendingDuration = 0
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("hls parse: %w", err)
	}
	return m, nil
}

// StitchOptions 缝合选项
type StitchOptions struct {
	SessionID  string
	BaseURL    string // 改写 segment URL 的前缀，e.g. https://ssai.adortb.com
	AdBreaks   []AdBreak
}

// AdBreak 广告片段集合
type AdBreak struct {
	Position int       // 插入位置（segment index）
	AdID     string
	Segments []Segment
}

// StitchHLS 把广告片段缝合进内容 manifest，返回新的 m3u8 文本
func StitchHLS(content *HLSManifest, opts StitchOptions) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-VERSION:%d\n", content.Version))
	sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(content.TargetDuration)+1))
	sb.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:0\n"))

	// 按位置排序广告 break（简单假设已排序）
	breakMap := make(map[int]AdBreak, len(opts.AdBreaks))
	for _, ab := range opts.AdBreaks {
		breakMap[ab.Position] = ab
	}

	for i, seg := range content.Segments {
		if ab, ok := breakMap[i]; ok {
			sb.WriteString("#EXT-X-DISCONTINUITY\n")
			for _, adSeg := range ab.Segments {
				sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", adSeg.Duration))
				// 改写 URL 到 tracking proxy
				trackURL := rewriteAdSegmentURL(opts.BaseURL, opts.SessionID, ab.AdID, adSeg.URI)
				sb.WriteString(trackURL + "\n")
			}
			sb.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration))
		sb.WriteString(seg.URI + "\n")
	}

	if content.IsEndList {
		sb.WriteString("#EXT-X-ENDLIST\n")
	}
	return sb.String()
}

// rewriteAdSegmentURL 把广告 segment URI 改写为 SSAI tracking proxy URL
func rewriteAdSegmentURL(baseURL, sessionID, adID, origURI string) string {
	// 取原始文件名
	parts := strings.Split(origURI, "/")
	filename := parts[len(parts)-1]
	return fmt.Sprintf("%s/v1/session/%s/ad_%s/%s", baseURL, sessionID, adID, filename)
}

// RenderHLS 把 HLSManifest 渲染为 m3u8 文本（不缝合广告，直接渲染）
func RenderHLS(m *HLSManifest) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-VERSION:%d\n", m.Version))
	sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(m.TargetDuration)))
	for _, seg := range m.Segments {
		sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration))
		sb.WriteString(seg.URI + "\n")
	}
	if m.IsEndList {
		sb.WriteString("#EXT-X-ENDLIST\n")
	}
	return sb.String()
}

// GenerateSlateManifest 生成 slate（兜底黑屏）manifest
func GenerateSlateManifest(durationSec float64, slateSegURL string) string {
	numSegs := int(durationSec/10) + 1
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:10\n")
	for i := 0; i < numSegs; i++ {
		sb.WriteString(fmt.Sprintf("#EXTINF:10.000,\n"))
		sb.WriteString(fmt.Sprintf("%s?t=%d\n", slateSegURL, time.Now().UnixNano()+int64(i)))
	}
	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}
