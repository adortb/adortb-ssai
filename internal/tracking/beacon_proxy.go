package tracking

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// BeaconRequest SSAI 代发的跟踪请求
type BeaconRequest struct {
	SessionID string    `json:"session_id"`
	AdID      string    `json:"ad_id"`
	Event     EventType `json:"event"`
	URLs      []string  `json:"urls"`
}

// BeaconProxy 负责代发广告平台要求的跟踪 beacon
type BeaconProxy struct {
	client *http.Client
	sem    chan struct{} // 并发限制
}

// NewBeaconProxy 创建 beacon 代发器，maxConcurrent 控制并发发送数
func NewBeaconProxy(maxConcurrent int) *BeaconProxy {
	if maxConcurrent <= 0 {
		maxConcurrent = 50
	}
	return &BeaconProxy{
		client: &http.Client{
			Timeout: 3 * time.Second,
			// 不跟随重定向（beacon 通常不需要）
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		sem: make(chan struct{}, maxConcurrent),
	}
}

// Fire 异步并发发送 beacon URLs
func (b *BeaconProxy) Fire(ctx context.Context, urls []string) {
	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			b.sem <- struct{}{}
			defer func() { <-b.sem }()
			b.send(ctx, url)
		}(u)
	}
	// 不等待完成（fire-and-forget）
}

// FireSync 同步发送 beacon（用于测试）
func (b *BeaconProxy) FireSync(ctx context.Context, urls []string) []error {
	errs := make([]error, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			b.sem <- struct{}{}
			defer func() { <-b.sem }()
			errs[idx] = b.send(ctx, url)
		}(i, u)
	}
	wg.Wait()
	return errs
}

func (b *BeaconProxy) send(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("beacon: build request %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "adortb-ssai/1.0")

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("beacon: send %s: %w", url, err)
	}
	defer resp.Body.Close()
	// 消费 body 以复用连接
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
