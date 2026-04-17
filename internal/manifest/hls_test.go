package manifest

import (
	"strings"
	"testing"
)

const sampleHLS = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
content_001.ts
#EXTINF:10.0,
content_002.ts
#EXT-X-CUE-OUT:30.000
#EXT-X-CUE-IN
#EXTINF:10.0,
content_003.ts
#EXT-X-ENDLIST`

func TestParseHLS_Basic(t *testing.T) {
	m, err := ParseHLS(sampleHLS)
	if err != nil {
		t.Fatalf("ParseHLS error: %v", err)
	}
	if m.Version != 3 {
		t.Errorf("version want 3 got %d", m.Version)
	}
	if m.TargetDuration != 10 {
		t.Errorf("targetDuration want 10 got %f", m.TargetDuration)
	}
	if len(m.Segments) != 3 {
		t.Errorf("segments want 3 got %d", len(m.Segments))
	}
	if len(m.CuePoints) != 1 {
		t.Fatalf("cuePoints want 1 got %d", len(m.CuePoints))
	}
	if m.CuePoints[0].Duration != 30 {
		t.Errorf("cue duration want 30 got %f", m.CuePoints[0].Duration)
	}
	if !m.IsEndList {
		t.Error("want IsEndList=true")
	}
}

func TestStitchHLS(t *testing.T) {
	m, _ := ParseHLS(sampleHLS)

	adSegs := []Segment{
		{URI: "https://cdn.adortb.com/ads/ad1/seg_001.ts", Duration: 10, IsAd: true, AdID: "ad1"},
		{URI: "https://cdn.adortb.com/ads/ad1/seg_002.ts", Duration: 10, IsAd: true, AdID: "ad1"},
	}
	breaks := []AdBreak{{Position: 2, AdID: "ad1", Segments: adSegs}}
	opts := StitchOptions{
		SessionID: "sess-001",
		BaseURL:   "https://ssai.adortb.com",
		AdBreaks:  breaks,
	}

	result := StitchHLS(m, opts)
	if !strings.Contains(result, "#EXT-X-DISCONTINUITY") {
		t.Error("stitched manifest should contain DISCONTINUITY tag")
	}
	if !strings.Contains(result, "/v1/session/sess-001/ad_ad1/") {
		t.Error("stitched manifest should rewrite ad segment URL")
	}
	if !strings.Contains(result, "content_001.ts") {
		t.Error("stitched manifest should contain content segments")
	}
}

func TestStitchHLS_NoAds(t *testing.T) {
	m, _ := ParseHLS(sampleHLS)
	result := StitchHLS(m, StitchOptions{BaseURL: "https://ssai.adortb.com"})
	if strings.Contains(result, "#EXT-X-DISCONTINUITY") {
		t.Error("no-ad manifest should not contain DISCONTINUITY")
	}
	if !strings.Contains(result, "content_001.ts") {
		t.Error("should still contain content segments")
	}
}

func TestRewriteAdSegmentURL(t *testing.T) {
	url := rewriteAdSegmentURL("https://ssai.adortb.com", "s1", "a1", "https://cdn.example.com/path/seg_001.ts")
	want := "https://ssai.adortb.com/v1/session/s1/ad_a1/seg_001.ts"
	if url != want {
		t.Errorf("want %s got %s", want, url)
	}
}

func TestSegmentCountForDuration(t *testing.T) {
	cases := []struct {
		dur     float64
		segDur  float64
		want    int
	}{
		{30, 10, 3},
		{31, 10, 4},
		{0, 10, 0},
	}
	for _, c := range cases {
		got := SegmentCountForDuration(c.dur, c.segDur)
		if got != c.want {
			t.Errorf("SegmentCount(%v,%v) want %d got %d", c.dur, c.segDur, c.want, got)
		}
	}
}
