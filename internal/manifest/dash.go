package manifest

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// MPD DASH manifest 结构
type MPD struct {
	XMLName               xml.Name  `xml:"MPD"`
	Xmlns                 string    `xml:"xmlns,attr"`
	Type                  string    `xml:"type,attr"`
	MediaPresentationDuration string `xml:"mediaPresentationDuration,attr,omitempty"`
	MinBufferTime         string    `xml:"minBufferTime,attr"`
	Periods               []Period  `xml:"Period"`
}

// Period DASH Period
type Period struct {
	ID       string       `xml:"id,attr,omitempty"`
	Start    string       `xml:"start,attr,omitempty"`
	Duration string       `xml:"duration,attr,omitempty"`
	AdSets   []AdaptationSet `xml:"AdaptationSet"`
}

// AdaptationSet DASH AdaptationSet
type AdaptationSet struct {
	ID              string         `xml:"id,attr"`
	MimeType        string         `xml:"mimeType,attr"`
	Codecs          string         `xml:"codecs,attr,omitempty"`
	Representations []Representation `xml:"Representation"`
}

// Representation DASH Representation
type Representation struct {
	ID        string    `xml:"id,attr"`
	Bandwidth int       `xml:"bandwidth,attr"`
	Width     int       `xml:"width,attr,omitempty"`
	Height    int       `xml:"height,attr,omitempty"`
	SegTemplate *SegmentTemplate `xml:"SegmentTemplate,omitempty"`
}

// SegmentTemplate DASH SegmentTemplate
type SegmentTemplate struct {
	Timescale      int    `xml:"timescale,attr"`
	Duration       int    `xml:"duration,attr,omitempty"`
	StartNumber    int    `xml:"startNumber,attr,omitempty"`
	Initialization string `xml:"initialization,attr,omitempty"`
	Media          string `xml:"media,attr,omitempty"`
}

// ParseDASH 解析 MPD XML 文本
func ParseDASH(content string) (*MPD, error) {
	var mpd MPD
	if err := xml.NewDecoder(strings.NewReader(content)).Decode(&mpd); err != nil {
		return nil, fmt.Errorf("dash parse: %w", err)
	}
	return &mpd, nil
}

// RenderDASH 将 MPD 渲染为 XML 文本
func RenderDASH(mpd *MPD) (string, error) {
	out, err := xml.MarshalIndent(mpd, "", "  ")
	if err != nil {
		return "", fmt.Errorf("dash render: %w", err)
	}
	return xml.Header + string(out), nil
}

// StitchDASH 在内容 MPD 中插入广告 Period
// adPeriods 会按 insertAfterPeriodIdx 插入到对应 content period 之后
func StitchDASH(content *MPD, adPeriods []Period, insertAfterPeriodIdx int) *MPD {
	newPeriods := make([]Period, 0, len(content.Periods)+len(adPeriods))
	for i, p := range content.Periods {
		newPeriods = append(newPeriods, p)
		if i == insertAfterPeriodIdx {
			newPeriods = append(newPeriods, adPeriods...)
		}
	}
	result := *content
	result.Periods = newPeriods
	return &result
}
