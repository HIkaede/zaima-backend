// Package database - MemoryCache 是一个轻量级的内存 TTL 缓存，
// 用于替代 Redis 在单机部署场景下的短期键值存储需求。
//
// 功能：Get / Set / SetNX / Del / Exists，均支持 TTL 自动过期。
// 使用 sync.RWMutex 保护并发安全，后台 goroutine 每分钟清理过期条目。
package database

import (
	"sync"
	"time"
)

// cacheEntry 单个缓存条目。
type cacheEntry struct {
	Value    string
	ExpireAt time.Time
}

// isExpired 检查条目是否过期。
func (e *cacheEntry) isExpired() bool {
	if e.ExpireAt.IsZero() {
		return false // 永不过期
	}
	return time.Now().After(e.ExpireAt)
}

// MemoryCache 线程安全的内存 TTL 缓存。
type MemoryCache struct {
	mu    sync.RWMutex
	store map[string]*cacheEntry
}

// NewMemoryCache 创建新的内存缓存实例并启动后台清理。
func NewMemoryCache() *MemoryCache {
	mc := &MemoryCache{
		store: make(map[string]*cacheEntry),
	}
	go mc.cleanupLoop()
	return mc
}

// Set 设置键值对，带过期时间。ttl <= 0 表示永不过期。
func (mc *MemoryCache) Set(key, value string, ttl time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	entry := &cacheEntry{Value: value}
	if ttl > 0 {
		entry.ExpireAt = time.Now().Add(ttl)
	}
	mc.store[key] = entry
}

// Get 获取键对应的值。如果不存在或已过期，返回 ("", ErrCacheMiss)。
func (mc *MemoryCache) Get(key string) (string, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	entry, ok := mc.store[key]
	if !ok || entry.isExpired() {
		return "", false
	}
	return entry.Value, true
}

// SetNX 仅在 key 不存在时设置。返回 true 表示设置成功 (key 之前不存在)。
func (mc *MemoryCache) SetNX(key, value string, ttl time.Duration) bool {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	existing, ok := mc.store[key]
	if ok && !existing.isExpired() {
		return false // key 已存在且未过期
	}

	entry := &cacheEntry{Value: value}
	if ttl > 0 {
		entry.ExpireAt = time.Now().Add(ttl)
	}
	mc.store[key] = entry
	return true
}

// Del 删除指定的 key。
func (mc *MemoryCache) Del(key string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	delete(mc.store, key)
}

// Exists 检查 key 是否存在且未过期。
func (mc *MemoryCache) Exists(key string) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	entry, ok := mc.store[key]
	return ok && !entry.isExpired()
}

// cleanupLoop 后台每60秒清理过期条目，防止内存泄漏。
func (mc *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		mc.mu.Lock()
		for key, entry := range mc.store {
			if entry.isExpired() {
				delete(mc.store, key)
			}
		}
		mc.mu.Unlock()
	}
}
