package slate

import (
	"fmt"
	"strings"
)

// SlateConfig 兜底 slate 配置
type SlateConfig struct {
	// BaseURL 是 slate segment 文件所在的 CDN 基础路径
	BaseURL     string
	SegDuration float64 // 每个 slate segment 时长（秒）
}

// Generator slate manifest 生成器
type Generator struct {
	cfg SlateConfig
}

// NewGenerator 创建 slate 生成器
func NewGenerator(cfg SlateConfig) *Generator {
	if cfg.SegDuration <= 0 {
		cfg.SegDuration = 10
	}
	return &Generator{cfg: cfg}
}

// GenerateHLS 生成 slate HLS manifest（黑屏填充）
// durationSec 是需要填充的广告时长
func (g *Generator) GenerateHLS(durationSec float64) string {
	if durationSec <= 0 {
		return ""
	}
	n := int(durationSec/g.cfg.SegDuration) + 1
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(g.cfg.SegDuration)))
	for i := 0; i < n; i++ {
		sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", g.cfg.SegDuration))
		sb.WriteString(fmt.Sprintf("%s/slate/black_%03d.ts\n", g.cfg.BaseURL, i+1))
	}
	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}

// SlateSegments 返回 slate segment URI 列表（用于 stitcher 直接插入）
func (g *Generator) SlateSegments(durationSec float64) []string {
	if durationSec <= 0 {
		return nil
	}
	n := int(durationSec/g.cfg.SegDuration) + 1
	uris := make([]string, n)
	for i := 0; i < n; i++ {
		uris[i] = fmt.Sprintf("%s/slate/black_%03d.ts", g.cfg.BaseURL, i+1)
	}
	return uris
}
