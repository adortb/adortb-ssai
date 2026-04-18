# adortb-ssai 内部架构

## 内部架构图

```
┌──────────────────────────────────────────────────────────────────┐
│                       adortb-ssai 内部架构                        │
│                                                                  │
│  播放器                                                          │
│    │ GET /v1/session/{sid}/master.m3u8?content={base64}          │
│    │ GET /v1/session/{sid}/ad_{adid}/{filename}                  │
│    ▼                                                             │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  internal/api/handler.go  Handler                        │   │
│  │  MasterManifest()  AdSegmentProxy()  TrackingBeacon()    │   │
│  │  SessionEvents()   DirectDecision()  Health()            │   │
│  └───────┬───────────────────┬────────────────┬─────────────┘   │
│          │                   │                │                 │
│   manifest处理           tracking          decision             │
│          │                   │                │                 │
│          ▼                   ▼                ▼                 │
│  ┌──────────────┐  ┌──────────────────┐  ┌──────────────┐      │
│  │  manifest/   │  │  tracking/       │  │  decision/   │      │
│  │  hls.go      │  │  session.go      │  │  client.go   │      │
│  │              │  │                  │  │              │      │
│  │  ParseHLS()  │  │  SessionStore    │  │  Decide()    │      │
│  │  StitchHLS() │  │  sync.Map+Redis  │  │  →adortb-adx │      │
│  │  RenderHLS() │  │  quartile event  │  │              │      │
│  └──────────────┘  └─────────┬────────┘  └──────────────┘      │
│                              │                                  │
│                    ┌─────────▼──────────┐                       │
│                    │  tracking/         │                       │
│                    │  beacon_proxy.go   │                       │
│                    │  BeaconProxy       │                       │
│                    │  并发池 async GET   │                       │
│                    └────────────────────┘                       │
│                                                                  │
│  ┌─────────────────┐    ┌─────────────┐                         │
│  │  transcoder/    │    │  slate/     │                         │
│  │  MockTranscoder │    │  Generator  │                         │
│  │  Transcode()    │    │  SlateSegs()│                         │
│  └─────────────────┘    └─────────────┘                         │
│                                                                  │
│  Redis（session TTL 24h）                                        │
└──────────────────────────────────────────────────────────────────┘
```

## 数据流

### 主 Manifest 请求数据流

```
播放器 GET /v1/session/{sid}/master.m3u8?content={base64_url}
    │
    ▼
Handler.MasterManifest()
    │
    ├─[1]─► base64.Decode(contentParam) → contentURL
    │
    ├─[2]─► sessions.GetOrCreate(sessionID, contentURL)
    │        本地 sync.Map 查找 → Redis 恢复 → 新建 SessionState
    │
    ├─[3]─► http.Get(contentURL) → HLS m3u8 text
    │
    ├─[4]─► manifest.ParseHLS(content)
    │        → HLSManifest{Segments, CuePoints, IsEndList}
    │
    ├─[5]─► decision.Client.Decide(ctx, AdDecision{duration, breakPositions})
    │        → DecisionResponse{Breaks[]{Ads[]{AdID, MediaURL, DurationSec}}}
    │        超时 300ms
    │
    ├─[6]─► buildAdBreaks(manifest, adResp)
    │        for each break:
    │          transcoder.Transcode(adID, mediaURL) → TranscodeResult{SegmentURIs}
    │          失败时 → slateGen.SlateSegments(duration)
    │        返回 []manifest.AdBreak
    │
    ├─[7]─► manifest.StitchHLS(m, StitchOptions{sessionID, baseURL, adBreaks})
    │        重写广告 segment URL → /v1/session/{sid}/ad_{adid}/{filename}
    │
    └─► 返回拼接后的 m3u8 文本
```

### Segment 代理数据流

```
播放器 GET /v1/session/{sid}/ad_{adid}/{filename}
    │
    ▼
Handler.AdSegmentProxy()
    │
    ├── async goroutine:
    │     sessions.RecordSegmentPlayed(sessionID, adID, totalSegs)
    │       → AdProgress[adID]++
    │       → quartileEvents(played, total) → []EventType
    │     beacon_proxy.Fire(ctx, beaconURLs)
    │       → 并发发送 HTTP GET beacon
    │
    └── proxySegment(origURL)
          http.Get(origURL) → io.Copy(w, resp.Body)
          Content-Type: video/mp2t
```

## 时序图

```
Player          Handler      SessionStore   Decision    ManifestStitcher   AdCDN
  │                │               │            │              │              │
  │─GET master.m3u8►               │            │              │              │
  │                │──GetOrCreate─►│            │              │              │
  │                │◄─session──────│            │              │              │
  │                │──fetchManifest(contentURL)─────────────────────────────► │
  │                │◄─m3u8 text─────────────────────────────────────────────  │
  │                │  ParseHLS()   │            │              │              │
  │                │──Decide()────────────────► │              │              │
  │                │◄─DecisionResp─────────────│              │              │
  │                │  buildAdBreaks() → Transcode/Slate        │              │
  │                │──StitchHLS()──────────────────────────────►              │
  │                │◄─stitched m3u8────────────────────────────               │
  │◄─m3u8──────────│               │            │              │              │
  │                │               │            │              │              │
  │─GET ad seg─────►               │            │              │              │
  │                │  RecordSegment►            │              │              │
  │                │  → quartile events         │              │              │
  │                │  BeaconProxy.Fire()        │              │              │
  │                │──proxySegment()────────────────────────────────────────► │
  │◄─ts binary──────────────────────────────────────────────────────────────  │
```

## 状态机

### SessionState 状态

```
不存在 → GetOrCreate() → active (Redis TTL=24h)
              │
              │ RecordSegmentPlayed()
              ▼
         AdProgress[adID]++
              │
              ├── played==1         → impression
              ├── played==total/4   → firstQuartile
              ├── played==total/2   → midpoint
              ├── played==3*total/4 → thirdQuartile
              └── played>=total     → complete
```

### HLS CUE 处理

```
m3u8 内容
    │
    ├── #EXT-X-CUE-OUT:30.0  → CuePoint{Duration:30, Position:N}
    │   [原始 segments 被忽略，由广告替换]
    └── #EXT-X-CUE-IN        → 结束 CUE 区间

StitchHLS:
    ├── CUE position → #EXT-X-DISCONTINUITY
    │                  [广告 segments（URL 改写为 SSAI 代理）]
    │                  #EXT-X-DISCONTINUITY
    └── 继续内容 segments
```

### 广告 Break 构建策略

```
DecisionResponse
    ├── 有广告决策 → Transcode(adID, mediaURL)
    │     ├── 成功 → 广告 segments
    │     └── 失败 → Slate 兜底
    └── 无广告决策 → slateBreaks()（所有 CUE 位置用 Slate 填充）
```
