# adortb-ssai

> adortb 平台服务端广告插入（SSAI）服务，在服务端将广告无缝拼接到 HLS m3u8 流，通过 segment 代理完成精确 VAST 事件上报。

## 快速理解

- **本项目做什么**：接收播放器 m3u8 请求 → 解析原始内容流 → 向 adortb-adx 请求广告决策 → 拼接广告分片 → 返回修改后的 m3u8；广告 segment 由 SSAI 代理，实现服务端跟踪
- **架构位置**：视频播放器与内容 CDN 之间的透明代理层
- **核心入口**：
  - 服务启动：`cmd/ssai/main.go`（端口 8107）
  - 主 API：`internal/api/handler.go:Handler.MasterManifest`
  - 会话跟踪：`internal/tracking/session.go:SessionStore`

## 目录结构

```
adortb-ssai/
├── cmd/ssai/main.go            # 主程序：Redis 连接，Handler 初始化
└── internal/
    ├── api/
    │   ├── handler.go          # HTTP Handler（MasterManifest/AdSegmentProxy/TrackingBeacon/SessionEvents/DirectDecision）
    │   ├── router.go           # 路由注册
    │   └── util.go             # JSON/路径工具
    ├── manifest/
    │   ├── hls.go              # ParseHLS / StitchHLS / RenderHLS（CUE-OUT 识别）
    │   ├── dash.go             # DASH MPD 支持
    │   └── stitcher.go         # AdBreak 构造（BuildAdBreak）
    ├── tracking/
    │   ├── session.go          # SessionStore（内存 sync.Map + Redis 持久化，quartile 计算）
    │   └── beacon_proxy.go     # BeaconProxy（并发池异步发送 HTTP beacon）
    ├── decision/
    │   ├── client.go           # adortb-adx 决策客户端（300ms 超时）
    │   └── cache.go            # 决策缓存
    ├── transcoder/
    │   ├── mock.go             # MockTranscoder（返回模拟 segment URL）
    │   └── profiles.go         # 转码规格
    └── slate/
        └── fallback.go         # Slate 生成（兜底黑屏 manifest）
```

## 核心概念

### HLS Manifest 解析（`manifest/hls.go`）

`ParseHLS(content)` 扫描 m3u8 文本，识别：
- `#EXT-X-CUE-OUT:30.0`：广告插入点（duration=30s）
- `#EXT-X-CUE-IN`：广告结束点
- 输出 `HLSManifest`（Segments + CuePoints）

### 广告缝合（`manifest/hls.go:StitchHLS`）

在 CuePoint 位置插入 AdBreak：
1. 在原始 segment 前插入 `#EXT-X-DISCONTINUITY`
2. 写入广告 segment（URL 改写为 SSAI 代理路由）
3. 再次 `#EXT-X-DISCONTINUITY` 恢复内容流

### 会话状态与 Quartile 触发（`tracking/session.go`）

```
SessionState.AdProgress[adID]  // 已播放 segment 数

RecordSegmentPlayed(sessionID, adID, totalSegs)
    → progress++ → quartileEvents(played, total)
    第 1 个 segment    → impression
    played == total/4  → firstQuartile
    played == total/2  → midpoint
    played == 3*total/4→ thirdQuartile
    played >= total    → complete
```

会话使用 `sync.Map` + Redis 双写（TTL 24h），进程重启可恢复。

### Slate 兜底

当 adortb-adx 无广告决策或转码失败时，`slate.Generator.SlateSegments` 生成黑屏占位 manifest，确保播放流不中断。

## 开发指南

### Go 版本

```bash
export PATH="$HOME/.goenv/versions/1.25.3/bin:$PATH"
```

### 本地运行

```bash
export REDIS_ADDR="localhost:6379"
export ADX_BASE_URL="http://localhost:8080"
export SELF_BASE_URL="http://localhost:8107"
export PORT=8107
go run cmd/ssai/main.go

# 测试 manifest 请求
CONTENT=$(echo -n "https://cdn.example.com/vod/test.m3u8" | base64)
curl "http://localhost:8107/v1/session/sess-001/master.m3u8?content=${CONTENT}"
```

### 测试

```bash
go test ./... -cover -race
go test ./internal/manifest/... -v    # HLS 解析/缝合测试
go test ./internal/tracking/... -v   # 会话/quartile 测试
```

### 代码约定

- 广告 segment 代理路由格式：`/v1/session/{sid}/ad_{adid}/{filename}`
- `buildAdBreaks` 优先使用真实广告，失败时 slate 兜底（不 panic）
- Beacon 发送使用 `BeaconProxy`（并发池），不阻塞请求处理
- WriteTimeout=30s（广告 segment 代理可能较慢）

## 依赖关系

- **上游**：视频播放器（HLS 请求），adortb-adx（广告决策）
- **下游**：Redis（会话持久化），内容 CDN（拉取原始 m3u8），广告 CDN（segment 代理）
- **依赖的库**：`go-redis/v9`，`alicebob/miniredis`（测试用 Redis Mock）

## 深入阅读

- CUE-OUT 解析细节：`internal/manifest/hls.go:ParseHLS`
- Segment URL 改写规则：`manifest/hls.go:rewriteAdSegmentURL`
- Quartile 计算逻辑：`tracking/session.go:quartileEvents`
- 转码 Mock 实现：`transcoder/mock.go:MockTranscoder.Transcode`
