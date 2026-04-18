# adortb-ssai

> adortb 平台的服务端广告插入（Server-Side Ad Insertion）服务，在服务器端将广告无缝拼接到 HLS/DASH 视频流，消除客户端广告拦截，并通过 segment 代理完成精确的 VAST 跟踪事件上报。

## 架构定位

```
┌─────────────────────────────────────────────────────────────────┐
│                      adortb 平台整体架构                         │
│                                                                  │
│  视频播放器（浏览器/OTT）                                        │
│       │ GET /v1/session/{id}/master.m3u8?content=<url>          │
│       ▼                                                         │
│  ★ adortb-ssai (SSAI Service)                                  │
│       │                                                         │
│  ┌────┼─────────────────────────────────────────────┐           │
│  │    ▼                                             │           │
│  │  [Manifest Parser]   解析 HLS m3u8（CUE-OUT标记） │           │
│  │       ↓                                          │           │
│  │  [Decision Client]   向 adortb-adx 请求广告决策   │           │
│  │       ↓                                          │           │
│  │  [Transcoder/Slate]  广告转码 or Slate 兜底        │           │
│  │       ↓                                          │           │
│  │  [Manifest Stitcher] 拼接广告 segment 到内容流    │           │
│  │       ↓                                          │           │
│  │  [Segment Proxy]     代理 ts 分片 + 触发跟踪事件  │           │
│  └─────────────────────────────────────────────────┘           │
│                                                                  │
│  Redis（播放会话持久化）                                         │
└─────────────────────────────────────────────────────────────────┘
```

SSAI 服务实现**透明广告插入**：播放器只看到一个连续的 m3u8，广告分片由 SSAI 代理投递，segment 播放时自动触发 impression/quartile 事件。

## 目录结构

```
adortb-ssai/
├── go.mod                          # Go 1.25.3，依赖 redis、miniredis（测试）
├── cmd/ssai/
│   └── main.go                     # 主程序：Redis 连接、Handler 初始化（端口 8107）
├── pkg/                            # 公共工具包
└── internal/
    ├── api/
    │   ├── handler.go              # HTTP 路由：manifest/segment/tracking/events/decision
    │   ├── router.go               # 路由注册
    │   └── util.go                 # JSON/路径工具
    ├── manifest/
    │   ├── hls.go                  # HLS m3u8 解析（ParseHLS）+ 渲染（RenderHLS）
    │   ├── dash.go                 # DASH MPD 支持
    │   └── stitcher.go             # 广告 break 缝合（StitchHLS）
    ├── tracking/
    │   ├── session.go              # SessionStore：内存 + Redis 会话，quartile 事件触发
    │   └── beacon_proxy.go         # 异步 beacon 发送（并发池）
    ├── decision/
    │   ├── client.go               # adortb-adx 广告决策客户端
    │   └── cache.go                # 决策结果缓存
    ├── transcoder/
    │   ├── mock.go                 # Mock Transcoder（开发/测试用）
    │   └── profiles.go             # 转码规格（分辨率/码率）
    └── slate/
        └── fallback.go             # Slate（兜底黑屏）manifest 生成
```

## 快速开始

### 环境要求

- Go 1.25.3
- Redis（会话持久化，不可用时降级为内存模式）

```bash
export PATH="$HOME/.goenv/versions/1.25.3/bin:$PATH"
```

### 运行服务

```bash
cd adortb-ssai

export REDIS_ADDR="localhost:6379"
export ADX_BASE_URL="http://localhost:8080"
export SELF_BASE_URL="https://ssai.adortb.com"
export SLATE_BASE_URL="https://cdn.adortb.com/slate"
export PORT=8107

go run cmd/ssai/main.go
```

### 运行测试

```bash
go test ./... -cover -race
```

## HTTP API

### GET /v1/session/{session_id}/master.m3u8?content={base64_url}

主 API：接收播放器请求，返回已插入广告的 m3u8 manifest。

**参数**：
- `session_id`：播放会话 ID（由客户端生成）
- `content`：原始内容 m3u8 URL（Base64URL 编码）

**示例**：
```bash
SESSION_ID=$(uuidgen)
CONTENT_URL=$(echo -n "https://cdn.example.com/vod/episode1.m3u8" | base64 -w0)
curl "https://ssai.adortb.com/v1/session/${SESSION_ID}/master.m3u8?content=${CONTENT_URL}"
```

响应为修改后的 m3u8，广告 segment 的 URL 指向 SSAI 代理。

### GET /v1/session/{session_id}/ad_{ad_id}/{filename}

广告 segment 代理端点：代理真实广告 ts 分片，同时触发跟踪事件。

### POST /v1/tracking/beacon

手动触发 VAST tracking beacon（批量 URL）。

```json
{"urls": ["https://track.example.com/impression?...", "..."]}
```

### GET /v1/session/{session_id}/events

查询会话已触发的所有播放事件。

### POST /v1/decision

直接向 adortb-adx 请求广告决策（调试用）。

### GET /health

```json
{"status": "ok"}
```

## 配置说明

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `REDIS_ADDR` | `localhost:6379` | Redis 地址 |
| `ADX_BASE_URL` | `http://localhost:8100` | adortb-adx 地址 |
| `SELF_BASE_URL` | `https://adx-ssai.adortb.com` | SSAI 自身对外地址（用于改写 segment URL） |
| `SLATE_BASE_URL` | `https://cdn.adortb.com` | Slate 兜底素材 CDN |
| `PORT` | `8107` | 监听端口 |

## Quartile 事件触发规则

SSAI 通过记录 segment 播放数来推算播放进度：

| 事件 | 触发条件 |
|------|---------|
| `impression` | 第 1 个 segment 播放 |
| `firstQuartile` | 播放 25% 分片 |
| `midpoint` | 播放 50% 分片 |
| `thirdQuartile` | 播放 75% 分片 |
| `complete` | 所有分片播放完 |

会话状态存储在 Redis（TTL 24h），进程重启后可恢复。

## 广告插入流程

```
播放器请求 master.m3u8
    │
    ▼
GetOrCreate Session（Redis/内存）
    │
    ▼
拉取原始内容 m3u8 → ParseHLS → 识别 CUE-OUT 广告位
    │
    ▼
向 adortb-adx 请求广告决策（300ms 超时）
    │
    ├─成功─► 广告转码/获取 segment URIs
    │
    └─失败─► Slate 兜底（黑屏占位）
    │
    ▼
StitchHLS：在 CUE-OUT 位置插入广告 breaks
改写广告 segment URL 为 SSAI 代理 URL
    │
    ▼
返回拼接后的 m3u8 给播放器
```

## 相关项目

| 项目 | 说明 |
|------|------|
| [adortb-adx](https://github.com/adortb/adortb-adx) | 广告决策引擎 |
| [adortb-common](https://github.com/adortb/adortb-common) | 公共工具库 |
| [adortb-infra](https://github.com/adortb/adortb-infra) | 基础设施 |
