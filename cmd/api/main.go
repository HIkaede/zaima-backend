// Package main 是 "在吗" APP 后端服务的入口。
//
// 启动流程:
//  1. 加载配置文件 (configs/config.yaml 或 config.standalone.yaml)
//  2. 初始化数据库 (PostgreSQL 或 SQLite) 与缓存 (Redis 或内存)
//  3. 启动 WebSocket Hub (独立 goroutine)
//  4. 注册路由并启动 HTTP 服务
//
// 命令行参数:
//
//	-config  配置文件路径 (默认 configs/config.yaml)
//	-port    监听端口 (覆盖配置文件，如 -port 9090)
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"zaima-backend/internal/config"
	"zaima-backend/internal/pkg/database"
	"zaima-backend/internal/router"
	"zaima-backend/internal/ws"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	portOverride := flag.Int("port", 0, "监听端口 (覆盖配置文件)")
	flag.Parse()

	// 1. 加载配置
	config.Load(*configPath)

	// 允许命令行 -port 覆盖配置文件端口
	if *portOverride > 0 {
		config.AppConfig.Server.Port = *portOverride
	}

	// 2. 设置 Gin 模式
	gin.SetMode(config.AppConfig.Server.Mode)

	// 3. 初始化数据库与缓存
	//    如果 database.path 配置了 SQLite 路径，使用单机模式 (SQLite + 内存缓存)
	//    否则使用 Docker 模式 (PostgreSQL + Redis)
	if config.AppConfig.Database.Path != "" {
		// 单机部署模式：SQLite + 内存缓存
		database.InitSQLite(config.AppConfig.Database.Path)
		database.InitCache()
		log.Println("[main] 运行模式: 单机部署 (SQLite + 内存缓存)")
	} else {
		// Docker 部署模式：PostgreSQL + Redis
		database.InitPostgres(&config.AppConfig.Database)
		database.InitRedis(&config.AppConfig.Redis)
		log.Println("[main] 运行模式: Docker 部署 (PostgreSQL + Redis)")
	}

	// 4. 启动 WebSocket Hub
	hub := ws.NewHub()
	go hub.Run()

	// 5. 初始化路由并启动 HTTP 服务
	r := router.SetupRouter(hub)

	addr := fmt.Sprintf(":%d", config.AppConfig.Server.Port)
	log.Printf("[main] 🚀 在吗后端服务启动: http://localhost%s", addr)
	log.Printf("[main] 📋 API 文档: http://localhost%s/health", addr)

	if err := r.Run(addr); err != nil {
		log.Fatalf("[main] 服务启动失败: %v", err)
	}
}
