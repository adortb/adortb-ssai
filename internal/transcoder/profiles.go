package transcoder

// CodecProfile 描述视频编码规格
type CodecProfile struct {
	Name       string
	Codec      string // h264, h265, av1, vp9
	Width      int
	Height     int
	Bitrate    int    // kbps
	FrameRate  float64
	AudioCodec string // aac, mp3
}

// StandardProfiles 预定义的编码规格
var StandardProfiles = []CodecProfile{
	{Name: "360p",  Codec: "h264", Width: 640,  Height: 360,  Bitrate: 800,  FrameRate: 25, AudioCodec: "aac"},
	{Name: "480p",  Codec: "h264", Width: 854,  Height: 480,  Bitrate: 1500, FrameRate: 25, AudioCodec: "aac"},
	{Name: "720p",  Codec: "h264", Width: 1280, Height: 720,  Bitrate: 3000, FrameRate: 30, AudioCodec: "aac"},
	{Name: "1080p", Codec: "h264", Width: 1920, Height: 1080, Bitrate: 6000, FrameRate: 30, AudioCodec: "aac"},
	{Name: "4k",    Codec: "h265", Width: 3840, Height: 2160, Bitrate: 15000,FrameRate: 30, AudioCodec: "aac"},
}

// MatchProfile 根据广告流参数选出最接近的编码规格
func MatchProfile(width, height, bitrate int) CodecProfile {
	best := StandardProfiles[0]
	minDiff := abs(StandardProfiles[0].Width-width) + abs(StandardProfiles[0].Height-height)
	for _, p := range StandardProfiles[1:] {
		diff := abs(p.Width-width) + abs(p.Height-height)
		if diff < minDiff {
			minDiff = diff
			best = p
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
