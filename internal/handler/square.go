// Package handler - "一起玩"广场气泡发布、发现、搜索与确认匹配。
package handler

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"zaima-backend/internal/model"
	"zaima-backend/internal/pkg/database"
	"zaima-backend/internal/pkg/response"
)

// ==================== 请求体定义 ====================

// PublishBubbleReq 发布气泡请求。
type PublishBubbleReq struct {
	VoiceURL    string  `json:"voice_url" binding:"required"` // OSS 录音文件 URL
	InterestTag string  `json:"interest_tag" binding:"required"`
	Province    string  `json:"province"`
	City        string  `json:"city"`
	Longitude   float64 `json:"longitude" binding:"required"`
	Latitude    float64 `json:"latitude" binding:"required"`
}

// MatchConfirmReq 确认匹配请求。
type MatchConfirmReq struct {
	BubbleID uint64 `json:"bubble_id" binding:"required"`
}

// ==================== Handler ====================

// PublishBubble 发布广场气泡。
// POST /api/v1/square/publish
//
// 流程：保存气泡到 DB -> 将经纬度写入 Redis GEO -> 设置4小时 TTL。
func PublishBubble(c *gin.Context) {
	userID := c.GetUint64("user_id")

	var req PublishBubbleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数不完整")
		return
	}

	// 查询用户信息 (昵称、头像)
	var user model.User
	database.DB.First(&user, userID)

	// 【安全】URL 白名单校验，防止 SSRF
	if !isValidOSSURL(req.VoiceURL) {
		response.BadRequest(c, "语音 URL 不合法，仅允许 OSS 地址")
		return
	}

	bubble := model.SquareBubble{
		UserID:      userID,
		Nickname:    user.Nickname,
		AvatarURL:   user.AvatarURL,
		VoiceURL:    req.VoiceURL,
		InterestTag: req.InterestTag,
		Province:    req.Province,
		City:        req.City,
		Longitude:   req.Longitude,
		Latitude:    req.Latitude,
		Status:      1, // 活跃
		ExpireAt:    time.Now().Add(4 * time.Hour),
	}

	if err := database.DB.Create(&bubble).Error; err != nil {
		response.ServerError(c, "发布失败，请稍后重试")
		return
	}

	// 写入 Redis GEO 用于地理位置范围查询 (若可用)
	ctx := context.Background()
	if database.RDB != nil {
		geoKey := "square:geo"
		memberKey := fmt.Sprintf("bubble:%d", bubble.ID)

		database.RDB.GeoAdd(ctx, geoKey, &redis.GeoLocation{
			Name:      memberKey,
			Longitude: req.Longitude,
			Latitude:  req.Latitude,
		})
	}
	// 设置4小时后自动过期 (使用单独的 key 标记 TTL)
	database.CacheSet(ctx, fmt.Sprintf("square:ttl:%d", bubble.ID), "1", 4*time.Hour)

	response.OKWithMsg(c, "发布成功", gin.H{"bubble_id": bubble.ID})
}

// GetBubbles 获取广场气泡列表。
// GET /api/v1/square/bubbles?lng=xxx&lat=xxx&radius=5000&province=xxx&city=xxx&keyword=xxx
//
// 支持：GEO 半径查询 + 省份/城市筛选 + 举办人昵称模糊搜索。
func GetBubbles(c *gin.Context) {
	lngStr := c.DefaultQuery("lng", "0")
	latStr := c.DefaultQuery("lat", "0")
	radiusStr := c.DefaultQuery("radius", "10000") // 默认10km
	province := c.Query("province")
	city := c.Query("city")
	keyword := c.Query("keyword")

	lng, _ := strconv.ParseFloat(lngStr, 64)
	lat, _ := strconv.ParseFloat(latStr, 64)
	radius, _ := strconv.ParseFloat(radiusStr, 64)

	ctx := context.Background()
	var bubbleIDs []uint64

	// 1. 优先使用 GEO 查询附近气泡
	if lng != 0 && lat != 0 {
		if database.RDB != nil {
			// Redis 环境下使用 GeoRadius
			results, err := database.RDB.GeoRadius(ctx, "square:geo", lng, lat, &redis.GeoRadiusQuery{
				Radius: radius,
				Unit:   "m",
				Sort:   "ASC",
				Count:  50,
			}).Result()
			if err == nil {
				for _, loc := range results {
					var id uint64
					_, _ = fmt.Sscanf(loc.Name, "bubble:%d", &id)
					if id > 0 {
						// 检查是否已过期
						if database.CacheExists(ctx, fmt.Sprintf("square:ttl:%d", id)) {
							bubbleIDs = append(bubbleIDs, id)
						}
					}
				}
			}
		} else {
			// SQLite/单机环境下使用内存里的距离计算 (Haversine 方式)
			var activeBubbles []model.SquareBubble
			database.DB.Where("status = 1 AND expire_at > ?", time.Now()).Find(&activeBubbles)
			for _, b := range activeBubbles {
				dist := haversineDistance(lat, lng, b.Latitude, b.Longitude)
				if dist <= radius {
					if database.CacheExists(ctx, fmt.Sprintf("square:ttl:%d", b.ID)) {
						bubbleIDs = append(bubbleIDs, b.ID)
					}
				}
			}
		}
	}

	// 2. 构建数据库查询
	query := database.DB.Model(&model.SquareBubble{}).Where("status = 1 AND expire_at > ?", time.Now())

	// 【修复 GEO 击穿】如果传入了 lng/lat 但附近没有气泡，应直接返回空列表
	geoSearched := lng != 0 && lat != 0
	if geoSearched && len(bubbleIDs) == 0 {
		// 附近无人，直接返回空
		response.OK(c, gin.H{"total": 0, "bubbles": []interface{}{}})
		return
	}
	if len(bubbleIDs) > 0 {
		query = query.Where("id IN ?", bubbleIDs)
	}

	// 省份/城市筛选
	if province != "" {
		query = query.Where("province = ?", province)
	}
	if city != "" {
		query = query.Where("city = ?", city)
	}

	// 举办人昵称模糊搜索
	if keyword != "" {
		query = query.Where("nickname LIKE ?", "%"+keyword+"%")
	}

	var bubbles []model.SquareBubble
	query.Order("created_at DESC").Limit(20).Find(&bubbles)

	response.OK(c, gin.H{
		"total":   len(bubbles),
		"bubbles": bubbles,
	})
}

// MatchConfirm 确认匹配 (发布者找到玩伴)。
// POST /api/v1/square/match-confirm
//
// 流程：标记气泡为已消失 -> 删除 Redis GEO 数据 -> 通知其他等待者聊天结束。
func MatchConfirm(c *gin.Context) {
	userID := c.GetUint64("user_id")

	var req MatchConfirmReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "缺少 bubble_id")
		return
	}

	var bubble model.SquareBubble
	if err := database.DB.First(&bubble, req.BubbleID).Error; err != nil {
		response.Fail(c, 2001, "气泡不存在")
		return
	}

	// 只有发布者能确认
	if bubble.UserID != userID {
		response.Fail(c, 2002, "无权操作此气泡")
		return
	}

	// 标记为已消失
	database.DB.Model(&bubble).Update("status", 0)

	// 清除 Redis GEO 和 TTL 数据
	ctx := context.Background()
	if database.RDB != nil {
		memberKey := fmt.Sprintf("bubble:%d", bubble.ID)
		database.RDB.ZRem(ctx, "square:geo", memberKey)
	}
	database.CacheDel(ctx, fmt.Sprintf("square:ttl:%d", bubble.ID))

	// TODO: 通过 WebSocket 通知其他正在和发布者聊天的用户：
	// 发送 { type: "match_ended", bubble_id: xxx, message: "对方已找到玩伴，聊天结束" }

	response.OKWithMsg(c, "匹配成功，气泡已消失", nil)
}

// haversineDistance 计算两个经纬度之间的距离，返回单位为米。
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000 // 地球半径(米)
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0

	lat1 = lat1 * math.Pi / 180.0
	lat2 = lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(lat1)*math.Cos(lat2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}
