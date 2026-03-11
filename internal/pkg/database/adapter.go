package database

import (
	"context"
	"time"
)

// CacheGet 统一缓存读取入口。优先使用内存缓存，否则使用 Redis。
func CacheGet(ctx context.Context, key string) (string, bool) {
	if Cache != nil {
		return Cache.Get(key)
	}
	if RDB != nil {
		val, err := RDB.Get(ctx, key).Result()
		return val, err == nil && val != ""
	}
	return "", false
}

// CacheSet 统一缓存写入入口。
func CacheSet(ctx context.Context, key, value string, ttl time.Duration) {
	if Cache != nil {
		Cache.Set(key, value, ttl)
		return
	}
	if RDB != nil {
		RDB.Set(ctx, key, value, ttl)
	}
}

// CacheSetNX 仅当 key 不存在时设置 (用于频控锁)。返回 true 表示加锁成功。
func CacheSetNX(ctx context.Context, key, value string, ttl time.Duration) bool {
	if Cache != nil {
		return Cache.SetNX(key, value, ttl)
	}
	if RDB != nil {
		locked, _ := RDB.SetNX(ctx, key, value, ttl).Result()
		return locked
	}
	return true
}

// CacheDel 从 Redis 或内存缓存删除 key。
func CacheDel(ctx context.Context, key string) {
	if Cache != nil {
		Cache.Del(key)
		return
	}
	if RDB != nil {
		RDB.Del(ctx, key)
	}
}

// CacheExists 检查 key 是否存在。
func CacheExists(ctx context.Context, key string) bool {
	if Cache != nil {
		return Cache.Exists(key)
	}
	if RDB != nil {
		n, _ := RDB.Exists(ctx, key).Result()
		return n > 0
	}
	return false
}
