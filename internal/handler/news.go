// Package handler - 天气与新闻查询 (带缓存与搜索)。
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"zaima-backend/internal/config"
	"zaima-backend/internal/model"
	"zaima-backend/internal/pkg/database"
	"zaima-backend/internal/pkg/response"
)

// ==================== Handler ====================

// GetWeather 获取子女所在城市的天气信息。
// GET /api/v1/weather?city=武汉
//
// 先从 Redis 缓存读取，失效后调用第三方 API 刷新。
func GetWeather(c *gin.Context) {
	city := c.Query("city")
	if city == "" {
		response.BadRequest(c, "缺少 city 参数")
		return
	}

	ctx := context.Background()
	cacheKey := fmt.Sprintf("weather:%s", city)

	// 1. 尝试读取缓存
	cached, ok := database.CacheGet(ctx, cacheKey)
	if ok && cached != "" {
		// 【修复】反序列化为 JSON 对象后返回
		var cachedData map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(cached), &cachedData); jsonErr == nil {
			response.OK(c, gin.H{
				"source": "cache",
				"data":   cachedData,
			})
			return
		}
	}

	// 2. 缓存未命中，调用第三方天气 API
	// TODO: 集成和风天气 SDK (https://devapi.qweather.com/v7/weather/now)
	// 伪数据兜底
	weatherData := gin.H{
		"city":     city,
		"temp":     "15°C",
		"text":     "多云",
		"icon":     "cloudy",
		"wind_dir": "东北风",
		"humidity": "65%",
		"tips":     "降温明显，记得多穿点衣服~",
		"updated":  time.Now().Format("2006-01-02 15:04"),
	}

	// 写入缓存
	ttl := time.Duration(config.AppConfig.Weather.CacheTTLHrs) * time.Hour
	if ttl == 0 {
		ttl = 12 * time.Hour
	}
	// 【修复】使用 json.Marshal 序列化存储，而非 fmt.Sprintf
	jsonBytes, _ := json.Marshal(weatherData)
	database.CacheSet(ctx, cacheKey, string(jsonBytes), ttl)

	response.OK(c, gin.H{
		"source": "api",
		"data":   weatherData,
	})
}

// GetCareCards 获取 AI 关怀气泡 (根据天气和时间段)。
// GET /api/v1/weather/care-cards?city=武汉
func GetCareCards(c *gin.Context) {
	// 根据时间段 + 天气条件生成关怀卡片文案
	now := time.Now()
	hour := now.Hour()

	cards := []gin.H{}

	// 根据时间段生成不同建议
	if hour >= 6 && hour < 9 {
		cards = append(cards, gin.H{"icon": "sunrise", "text": "早上好，记得吃早餐哦~"})
	}
	if hour >= 11 && hour < 14 {
		cards = append(cards, gin.H{"icon": "bowl", "text": "工作再忙也要按时吃饭~"})
	}
	if hour >= 22 || hour < 5 {
		cards = append(cards, gin.H{"icon": "moon", "text": "早点休息，别熬夜~"})
	}

	// TODO: 根据实际天气数据动态调整，如降温预警、暴雨提醒等
	cards = append(cards, gin.H{"icon": "heart", "text": "想你了，有空打个电话~"})

	response.OK(c, gin.H{"cards": cards})
}

// GetNews 获取孩子所在城市的新闻列表。
// GET /api/v1/news?city=武汉&tab=all&keyword=xxx&page=1&page_size=10
//
// 支持 tab 分类 (all/latest/hot) 和关键字搜索，带半天 Redis 缓存。
func GetNews(c *gin.Context) {
	city := c.DefaultQuery("city", "")
	tab := c.DefaultQuery("tab", "all") // all / latest / hot
	keyword := c.DefaultQuery("keyword", "")
	page := c.DefaultQuery("page", "1")
	pageSize := c.DefaultQuery("page_size", "10")

	if city == "" {
		// 兜底：返回全国热门新闻
		city = "全国"
	}

	ctx := context.Background()
	cacheKey := fmt.Sprintf("news:%s:%s", city, tab)

	// 1. 尝试读取缓存 (无关键字搜索时)
	if keyword == "" {
		cached, ok := database.CacheGet(ctx, cacheKey)
		if ok && cached != "" {
			response.OK(c, gin.H{
				"source": "cache",
				"city":   city,
				"tab":    tab,
				"data":   cached,
			})
			return
		}
	}

	// 2. 从数据库查询缓存的新闻
	query := database.DB.Model(&model.NewsCache{}).Where("city = ?", city)

	if tab == "latest" {
		query = query.Order("pub_date DESC")
	} else if tab == "hot" {
		query = query.Order("created_at DESC") // 热点按抓取时间降序
	}

	if keyword != "" {
		query = query.Where("title LIKE ?", "%"+keyword+"%")
	}

	// 【修复】启用支付分页参数
	pageNum, _ := strconv.Atoi(page)
	pageSz, _ := strconv.Atoi(pageSize)
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSz < 1 || pageSz > 50 {
		pageSz = 10
	}

	var news []model.NewsCache
	query.Offset((pageNum - 1) * pageSz).Limit(pageSz).Find(&news)

	if len(news) == 0 {
		// TODO: 触发异步抓取新闻任务
		response.OK(c, gin.H{
			"city":    city,
			"tab":     tab,
			"data":    []interface{}{},
			"message": "暂无新闻，正在为您抓取最新资讯...",
		})
		return
	}

	response.OK(c, gin.H{
		"city":  city,
		"tab":   tab,
		"total": len(news),
		"data":  news,
	})
}
