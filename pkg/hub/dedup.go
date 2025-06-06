package hub

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MessageID 消息唯一标识符
type MessageID struct {
	NodeID    string `json:"node_id"`   // 节点ID
	Timestamp int64  `json:"timestamp"` // 时间戳
	Sequence  uint64 `json:"sequence"`  // 序列号
}

// String 返回消息ID的字符串表示
func (m MessageID) String() string {
	return fmt.Sprintf("%s-%d-%d", m.NodeID, m.Timestamp, m.Sequence)
}

type BusMessage struct {
	ID            MessageID       `json:"id"`                // 消息唯一ID
	Type          string          `json:"type"`              // 消息类型(broadcast, room, unicast)
	RoomID        string          `json:"room_id,omitempty"` // 房间ID(如果适用)
	ExcludeClient string          `json:"exclude,omitempty"` // 排除的客户端ID(如果适用)
	Payload       json.RawMessage `json:"payload"`           // 实际消息内容
	SentAt        time.Time       `json:"sent_at"`           // 发送时间
}

// MessageDeduplicator 消息去重器
type MessageDeduplicator struct {
	nodeID      string        // 当前节点ID(实例标识)
	cache       sync.Map      // 已处理消息缓存 key=MessageID.String(), value=time.Time
	sequence    uint64        // 消息序列号
	cleanupMu   sync.Mutex    // 清理互斥锁
	lastCleanup time.Time     // 上次清理时间
	ttl         time.Duration // 消息缓存生存时间
}

// NewMessageDeduplicator 消息去重器
func NewMessageDeduplicator(nodeID string, ttl time.Duration) *MessageDeduplicator {
	if ttl <= 0 {
		ttl = 30 * time.Second // 默认30秒
	}

	dedup := &MessageDeduplicator{
		nodeID:      nodeID,
		ttl:         ttl,
		lastCleanup: time.Now(),
	}

	return dedup
}

// GenerateID 生成新的消息ID
func (d *MessageDeduplicator) GenerateID() MessageID {
	seq := atomic.AddUint64(&d.sequence, 1)
	return MessageID{
		NodeID:    d.nodeID,
		Timestamp: time.Now().UnixNano(),
		Sequence:  seq,
	}
}

// IsDuplicate 检查消息是否重复
func (d *MessageDeduplicator) IsDuplicate(msgID string) bool {
	_, exists := d.cache.Load(msgID)
	return exists
}

// MarkProcessed 标记消息已处理
func (d *MessageDeduplicator) MarkProcessed(msgID string) {
	d.cache.Store(msgID, time.Now())

	// 每处理100条消息，尝试清理过期的缓存
	if atomic.LoadUint64(&d.sequence)%100 == 0 {
		d.cleanExpired()
	}
}

// cleanExpired 清理过期的消息缓存
func (d *MessageDeduplicator) cleanExpired() {
	// 避免多个goroutine同时清理
	if !d.cleanupMu.TryLock() {
		return
	}
	defer d.cleanupMu.Unlock()

	// 限制清理频率
	if time.Since(d.lastCleanup) < time.Minute {
		return
	}

	now := time.Now()
	d.lastCleanup = now

	d.cache.Range(func(key, value interface{}) bool {
		processTime, ok := value.(time.Time)
		if !ok {
			// 移除格式错误的记录
			d.cache.Delete(key)
			return true
		}

		// 删除过期记录
		if now.Sub(processTime) > d.ttl {
			d.cache.Delete(key)
		}
		return true
	})
}
