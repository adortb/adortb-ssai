package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultTTL    = 5 * time.Minute
	keyPrefix     = "ssai:decision:"
)

// Cache 广告决策缓存（基于 Redis）
type Cache struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewCache 创建缓存实例
func NewCache(rdb *redis.Client) *Cache {
	return &Cache{rdb: rdb, ttl: defaultTTL}
}

// WithTTL 设置自定义 TTL
func (c *Cache) WithTTL(d time.Duration) *Cache {
	c.ttl = d
	return c
}

// Get 从缓存中获取广告决策
func (c *Cache) Get(ctx context.Context, key string) (*DecisionResponse, bool) {
	val, err := c.rdb.Get(ctx, keyPrefix+key).Bytes()
	if err != nil {
		return nil, false
	}
	var resp DecisionResponse
	if err := json.Unmarshal(val, &resp); err != nil {
		return nil, false
	}
	return &resp, true
}

// Set 将广告决策写入缓存
func (c *Cache) Set(ctx context.Context, key string, resp *DecisionResponse) error {
	val, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("cache set: marshal: %w", err)
	}
	return c.rdb.Set(ctx, keyPrefix+key, val, c.ttl).Err()
}

// CacheKey 生成缓存 key
func CacheKey(sessionID, breakPos string) string {
	return fmt.Sprintf("%s:%s", sessionID, breakPos)
}
