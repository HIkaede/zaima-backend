// Package database 提供数据库与缓存的连接初始化。
// 支持 PostgreSQL (Docker 部署) 和 SQLite (单机部署) 两种模式。
package database

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"zaima-backend/internal/config"
	"zaima-backend/internal/model"
)

// DB 全局数据库连接实例。
var DB *gorm.DB

// RDB 全局 Redis 客户端实例 (Docker 部署模式下使用)。
var RDB *redis.Client

// Cache 全局内存缓存实例 (单机部署模式下使用)。
var Cache *MemoryCache

// autoMigrateAll 自动迁移所有模型 (创建/更新表结构)。
func autoMigrateAll() {
	if err := DB.AutoMigrate(
		&model.User{},
		&model.UserRelation{},
		&model.UserInterest{},
		&model.DeviceStatusLog{},
		&model.MonthlyReport{},
		&model.ChatMessage{},
		&model.SquareBubble{},
		&model.NewsCache{},
	); err != nil {
		log.Fatalf("[database] AutoMigrate 失败: %v", err)
	}
}

// InitPostgres 初始化 PostgreSQL 连接并执行自动建表。
func InitPostgres(cfg *config.DatabaseConfig) {
	var err error
	DB, err = gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatalf("[database] PostgreSQL 连接失败: %v", err)
	}

	sqlDB, _ := DB.DB()
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	autoMigrateAll()
	log.Println("[database] PostgreSQL 连接成功，表结构已同步")
}

// InitSQLite 初始化 SQLite 连接并执行自动建表。
// 适用于单机部署，无需外部数据库服务。
func InitSQLite(dbPath string) {
	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("[database] SQLite 打开失败: %v", err)
	}

	// SQLite 性能优化
	sqlDB, _ := DB.DB()
	sqlDB.SetMaxOpenConns(1) // SQLite 不支持并发写
	sqlDB.SetMaxIdleConns(1)

	// 开启 WAL 模式，提升并发读性能
	DB.Exec("PRAGMA journal_mode=WAL")
	DB.Exec("PRAGMA synchronous=NORMAL")

	autoMigrateAll()
	log.Printf("[database] SQLite 数据库已就绪: %s", dbPath)
}

// InitRedis 初始化 Redis 连接。
func InitRedis(cfg *config.RedisConfig) {
	RDB = redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if _, err := RDB.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("[database] Redis 连接失败: %v", err)
	}
	log.Println("[database] Redis 连接成功")
}

// InitCache 初始化内存缓存 (替代 Redis，用于单机部署)。
func InitCache() {
	Cache = NewMemoryCache()
	log.Println("[database] 内存缓存已就绪 (standalone 模式)")
}
