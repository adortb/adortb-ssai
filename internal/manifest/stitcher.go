package manifest

import (
	"fmt"
)

// BreakSpec 描述一个广告 break 的位置和时长
type BreakSpec struct {
	PositionSec  float64
	DurationSec  float64
}

// FindCuePoints 从 HLSManifest 中提取广告 break 位置（按累计时长）
func FindCuePoints(m *HLSManifest) []CuePoint {
	return m.CuePoints
}

// BuildAdBreak 把广告 segments 组装成 AdBreak（position 为 content segment 插入点）
func BuildAdBreak(position int, adID string, adSegURIs []string, segDuration float64) AdBreak {
	segs := make([]Segment, 0, len(adSegURIs))
	for _, uri := range adSegURIs {
		segs = append(segs, Segment{
			URI:      uri,
			Duration: segDuration,
			IsAd:     true,
			AdID:     adID,
		})
	}
	return AdBreak{
		Position: position,
		AdID:     adID,
		Segments: segs,
	}
}

// SegmentCountForDuration 估算广告需要多少个 segment（向上取整）
func SegmentCountForDuration(durationSec, segDurationSec float64) int {
	if segDurationSec <= 0 {
		return 0
	}
	n := int(durationSec / segDurationSec)
	if durationSec > float64(n)*segDurationSec {
		n++
	}
	return n
}

// MockAdSegURIs 生成 mock 广告 segment URI 列表（开发/测试用）
func MockAdSegURIs(adID string, count int) []string {
	uris := make([]string, count)
	for i := 0; i < count; i++ {
		uris[i] = fmt.Sprintf("https://cdn.adortb.com/ads/%s/seg_%03d.ts", adID, i+1)
	}
	return uris
}
