package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// EventType 播放事件类型
type EventType string

const (
	EventImpression  EventType = "impression"
	EventFirstQuartile EventType = "firstQuartile"
	EventMidpoint    EventType = "midpoint"
	EventThirdQuartile EventType = "thirdQuartile"
	EventComplete    EventType = "complete"
	EventClick       EventType = "click"

	sessionKeyPrefix = "ssai:session:"
	sessionTTL       = 24 * time.Hour
)

// PlayEvent 播放事件记录
type PlayEvent struct {
	Type      EventType `json:"type"`
	AdID      string    `json:"ad_id"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// SessionState 会话状态
type SessionState struct {
	SessionID   string            `json:"session_id"`
	ContentURL  string            `json:"content_url"`
	CreatedAt   time.Time         `json:"created_at"`
	AdProgress  map[string]int    `json:"ad_progress"` // adID -> 已播放 segment 数
	Events      []PlayEvent       `json:"events"`
	mu          sync.Mutex        `json:"-"`
}

// SessionStore 会话存储（内存 + Redis 持久化）
type SessionStore struct {
	rdb      *redis.Client
	local    sync.Map // sessionID -> *SessionState
}

// NewSessionStore 创建会话存储
func NewSessionStore(rdb *redis.Client) *SessionStore {
	return &SessionStore{rdb: rdb}
}

// GetOrCreate 获取或创建播放会话
func (s *SessionStore) GetOrCreate(ctx context.Context, sessionID, contentURL string) (*SessionState, error) {
	if v, ok := s.local.Load(sessionID); ok {
		return v.(*SessionState), nil
	}

	// 从 Redis 恢复
	if sess, err := s.loadFromRedis(ctx, sessionID); err == nil {
		s.local.Store(sessionID, sess)
		return sess, nil
	}

	sess := &SessionState{
		SessionID:  sessionID,
		ContentURL: contentURL,
		CreatedAt:  time.Now(),
		AdProgress: make(map[string]int),
	}
	s.local.Store(sessionID, sess)
	if err := s.persist(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// RecordSegmentPlayed 记录广告 segment 播放，返回触发的事件列表
func (s *SessionStore) RecordSegmentPlayed(ctx context.Context, sessionID, adID string, totalSegs int) ([]EventType, error) {
	v, ok := s.local.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	sess := v.(*SessionState)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	sess.AdProgress[adID]++
	played := sess.AdProgress[adID]

	var triggered []EventType

	// 第一个 segment 触发 impression
	if played == 1 {
		triggered = append(triggered, EventImpression)
	}
	// quartile 事件
	triggered = append(triggered, quartileEvents(played, totalSegs)...)

	// 记录事件
	for _, et := range triggered {
		sess.Events = append(sess.Events, PlayEvent{
			Type:      et,
			AdID:      adID,
			SessionID: sessionID,
			Timestamp: time.Now(),
		})
	}

	_ = s.persist(ctx, sess)
	return triggered, nil
}

// GetEvents 获取会话所有事件
func (s *SessionStore) GetEvents(ctx context.Context, sessionID string) ([]PlayEvent, error) {
	v, ok := s.local.Load(sessionID)
	if ok {
		sess := v.(*SessionState)
		sess.mu.Lock()
		defer sess.mu.Unlock()
		cp := make([]PlayEvent, len(sess.Events))
		copy(cp, sess.Events)
		return cp, nil
	}

	sess, err := s.loadFromRedis(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return sess.Events, nil
}

func (s *SessionStore) persist(ctx context.Context, sess *SessionState) error {
	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, sessionKeyPrefix+sess.SessionID, b, sessionTTL).Err()
}

func (s *SessionStore) loadFromRedis(ctx context.Context, sessionID string) (*SessionState, error) {
	b, err := s.rdb.Get(ctx, sessionKeyPrefix+sessionID).Bytes()
	if err != nil {
		return nil, err
	}
	var sess SessionState
	if err := json.Unmarshal(b, &sess); err != nil {
		return nil, err
	}
	sess.AdProgress = make(map[string]int)
	return &sess, nil
}

// quartileEvents 根据已播放 segment 数计算应触发的 quartile 事件
func quartileEvents(played, total int) []EventType {
	if total == 0 {
		return nil
	}
	var evts []EventType
	pct := float64(played) / float64(total)
	// 使用精确比较避免重复触发（每个 quartile 只触发一次）
	if played == (total+3)/4 && pct <= 0.26 {
		evts = append(evts, EventFirstQuartile)
	}
	if played == total/2 && pct <= 0.51 {
		evts = append(evts, EventMidpoint)
	}
	if played == (3*total)/4 && pct <= 0.76 {
		evts = append(evts, EventThirdQuartile)
	}
	if played >= total {
		evts = append(evts, EventComplete)
	}
	return evts
}
