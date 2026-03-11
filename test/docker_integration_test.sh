#!/bin/bash
# =============================================================
# "在吗" API 集成测试脚本 (真实 Docker 环境)
# 测试完整用户流程：注册→登录→资料→绑定→聊天→广场→天气→新闻
# =============================================================

set -e
BASE="http://localhost:8080"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'
PASSED=0
FAILED=0

assert_status() {
    local desc="$1" expected="$2" actual="$3" body="$4"
    if [ "$expected" = "$actual" ]; then
        echo -e "${GREEN}✅ PASS${NC}: $desc (HTTP $actual)"
        PASSED=$((PASSED+1))
    else
        echo -e "${RED}❌ FAIL${NC}: $desc (expected HTTP $expected, got $actual)"
        echo "  Response: $body"
        FAILED=$((FAILED+1))
    fi
}

assert_contains() {
    local desc="$1" expected="$2" body="$3" status="$4"
    if echo "$body" | grep -q "$expected"; then
        echo -e "${GREEN}✅ PASS${NC}: $desc (contains '$expected')"
        PASSED=$((PASSED+1))
    else
        echo -e "${RED}❌ FAIL${NC}: $desc (expected body to contain '$expected')"
        echo "  Response: $body (HTTP $status)"
        FAILED=$((FAILED+1))
    fi
}

echo "=========================================="
echo "  在吗 API 集成测试 - Docker 环境"
echo "=========================================="
echo ""

# ==================== 1. Health Check ====================
echo -e "${YELLOW}[1] Health Check${NC}"
RESP=$(curl -s -w "\n%{http_code}" "$BASE/health")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "GET /health" 200 "$STATUS" "$BODY"
assert_contains "health status ok" '"ok"' "$BODY" "$STATUS"
echo ""

# ==================== 2. Auth: SMS + Login (老人) ====================
echo -e "${YELLOW}[2] Auth: 老人注册/登录${NC}"
# 使用时间戳生成唯一的手机号，防止多次测试时数据冲突
SUFFIX=$(date +%s | cut -c 7-10)
ELDER_PHONE="138000${SUFFIX}1"

# 发送验证码
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/auth/sms-code" \
  -H "Content-Type: application/json" \
  -d "{\"phone\":\"$ELDER_PHONE\"}")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "POST /auth/sms-code (老人)" 200 "$STATUS" "$BODY"

# 提取验证码 (开发模式下直接从响应中拿，如果是生产需要去读日志或 Redis，这里走通用逻辑)
ELDER_CODE=$(echo "$BODY" | jq -r '.data.code // empty')
if [ -z "$ELDER_CODE" ]; then
    # 如果接口没直接返回 (例如非 debug 模式)，尝试从本地 Redis 读以便兼容旧 Docker 模式
    ELDER_CODE=$(docker exec zaima-redis redis-cli GET "sms:code:$ELDER_PHONE" 2>/dev/null | tr -d '"')
fi
echo "  📱 老人验证码: $ELDER_CODE"

# 登录
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"phone\":\"$ELDER_PHONE\",\"code\":\"$ELDER_CODE\",\"role\":1}")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "POST /auth/login (老人)" 200 "$STATUS" "$BODY"
assert_contains "login returns token" "token" "$BODY" "$STATUS"
ELDER_TOKEN=$(echo "$BODY" | jq -r '.data.token')
ELDER_ID=$(echo "$BODY" | jq -r '.data.user_id')
echo "  🔑 老人 Token: ${ELDER_TOKEN:0:30}..."
echo "  🆔 老人 ID: $ELDER_ID"
echo ""

# ==================== 3. Auth: SMS + Login (年轻人) ====================
echo -e "${YELLOW}[3] Auth: 年轻人注册/登录${NC}"
YOUTH_PHONE="138000${SUFFIX}2"

RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/auth/sms-code" \
  -H "Content-Type: application/json" \
  -d "{\"phone\":\"$YOUTH_PHONE\"}")
STATUS=$(echo "$RESP" | tail -1)
assert_status "POST /auth/sms-code (年轻人)" 200 "$STATUS"

BODY=$(echo "$RESP" | head -1)

YOUTH_CODE=$(echo "$BODY" | jq -r '.data.code // empty')
if [ -z "$YOUTH_CODE" ]; then
    YOUTH_CODE=$(docker exec zaima-redis redis-cli GET "sms:code:$YOUTH_PHONE" 2>/dev/null | tr -d '"')
fi

RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"phone\":\"$YOUTH_PHONE\",\"code\":\"$YOUTH_CODE\",\"role\":2}")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "POST /auth/login (年轻人)" 200 "$STATUS" "$BODY"
YOUTH_TOKEN=$(echo "$BODY" | jq -r '.data.token')
YOUTH_ID=$(echo "$BODY" | jq -r '.data.user_id')
echo "  🔑 年轻人 Token: ${YOUTH_TOKEN:0:30}..."
echo "  🆔 年轻人 ID: $YOUTH_ID"
echo ""

# ==================== 4. Profile ====================
echo -e "${YELLOW}[4] Profile: 更新/获取资料${NC}"

# 更新老人资料
RESP=$(curl -s -w "\n%{http_code}" -X PUT "$BASE/api/v1/user/profile" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d '{"nickname":"张奶奶","city":"武汉"}')
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "PUT /user/profile (老人)" 200 "$STATUS" "$BODY"

# 获取资料
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/user/profile" \
  -H "Authorization: Bearer $ELDER_TOKEN")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "GET /user/profile (老人)" 200 "$STATUS" "$BODY"
assert_contains "profile has nickname" "张奶奶" "$BODY" "$STATUS"

# 更新年轻人资料
RESP=$(curl -s -w "\n%{http_code}" -X PUT "$BASE/api/v1/user/profile" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $YOUTH_TOKEN" \
  -d '{"nickname":"小明","city":"北京"}')
STATUS=$(echo "$RESP" | tail -1)
assert_status "PUT /user/profile (年轻人)" 200 "$STATUS"
echo ""

# ==================== 5. SSRF URL 校验 ====================
echo -e "${YELLOW}[5] Security: SSRF URL 校验${NC}"

# 内网地址应被拦截
RESP=$(curl -s -w "\n%{http_code}" -X PUT "$BASE/api/v1/user/profile" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d '{"avatar_url":"http://169.254.169.254/latest/meta-data/"}')
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "SSRF: 内网地址被拦截" 400 "$STATUS" "$BODY"

# 合法 OSS 地址应通过
RESP=$(curl -s -w "\n%{http_code}" -X PUT "$BASE/api/v1/user/profile" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d '{"avatar_url":"https://zaima.oss.aliyuncs.com/avatar/test.jpg"}')
STATUS=$(echo "$RESP" | tail -1)
assert_status "SSRF: OSS 地址通过" 200 "$STATUS"
echo ""

# ==================== 6. Binding ====================
echo -e "${YELLOW}[6] Binding: 亲子绑定流程${NC}"

# 老人发起绑定
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/user/bind" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d "{\"target_phone\":\"$YOUTH_PHONE\",\"remark\":\"孙子\"}")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "POST /user/bind (老人发起)" 200 "$STATUS" "$BODY"
assert_contains "bind pending" "pending" "$BODY" "$STATUS"
RELATION_ID=$(echo "$BODY" | jq -r '.data.relation_id')
echo "  🔗 绑定关系 ID: $RELATION_ID"

# 重复绑定应拒绝
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/user/bind" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d "{\"target_phone\":\"$YOUTH_PHONE\",\"remark\":\"孙子\"}")
BODY=$(echo "$RESP" | head -1)
assert_contains "duplicate bind rejected" "1010" "$BODY"

# 老人自己确认应拒绝 (自确认攻击)
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/user/bind/confirm" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d "{\"relation_id\":$RELATION_ID}")
BODY=$(echo "$RESP" | head -1)
assert_contains "self-confirm rejected" "1007" "$BODY"

# 年轻人确认绑定
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/user/bind/confirm" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $YOUTH_TOKEN" \
  -d "{\"relation_id\":$RELATION_ID}")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "POST /user/bind/confirm (年轻人确认)" 200 "$STATUS" "$BODY"

# 重复确认应拒绝 (幂等)
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/user/bind/confirm" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $YOUTH_TOKEN" \
  -d "{\"relation_id\":$RELATION_ID}")
BODY=$(echo "$RESP" | head -1)
assert_contains "idempotent confirm rejected" "1009" "$BODY"

echo ""

# ==================== 7. Interests ====================
echo -e "${YELLOW}[7] Interests: 兴趣标签${NC}"

RESP=$(curl -s -w "\n%{http_code}" -X PUT "$BASE/api/v1/user/interests" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d '{"tags":["太极拳","广场舞","下棋"]}')
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "PUT /user/interests" 200 "$STATUS" "$BODY"
echo ""

# ==================== 8. Device Monitoring ====================
echo -e "${YELLOW}[8] Device: 设备数据上传/查询${NC}"

# 上传设备数据
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/device/upload" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ELDER_TOKEN" \
  -d '{"record_date":"2026-03-05","steps":5800,"battery_level":72,"screen_unlocks":15,"screen_usage_mins":120}')
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "POST /device/upload" 200 "$STATUS" "$BODY"

# 年轻人查看老人今日数据 (有绑定关系)
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/device/daily-insight?elder_id=$ELDER_ID" \
  -H "Authorization: Bearer $YOUTH_TOKEN")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "GET /device/daily-insight" 200 "$STATUS" "$BODY"
assert_contains "daily insight has steps" "5800" "$BODY" "$STATUS"
echo ""

# ==================== 9. Chat IDOR ====================
echo -e "${YELLOW}[9] Security: Chat IDOR${NC}"

# 第三个用户, 未绑定
HACKER_PHONE="138000${SUFFIX}9"
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/auth/sms-code" \
  -H "Content-Type: application/json" \
  -d "{\"phone\":\"$HACKER_PHONE\"}")
BODY=$(echo "$RESP" | head -1)
HACKER_CODE=$(echo "$BODY" | jq -r '.data.code // empty')
if [ -z "$HACKER_CODE" ]; then
    HACKER_CODE=$(docker exec zaima-redis redis-cli GET "sms:code:$HACKER_PHONE" 2>/dev/null | tr -d '"')
fi
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"phone\":\"$HACKER_PHONE\",\"code\":\"$HACKER_CODE\",\"role\":2}")
BODY=$(echo "$RESP" | head -1)
HACKER_TOKEN=$(echo "$BODY" | jq -r '.data.token')

# 未绑定用户尝试查看老人聊天记录 -> 403
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/chat/history?peer_id=$ELDER_ID" \
  -H "Authorization: Bearer $HACKER_TOKEN")
BODY=$(echo "$RESP" | head -1)
assert_contains "IDOR: 未绑定用户查看聊天被拒" "403" "$BODY"

# 未绑定用户查看 AI 建议 -> 403
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/chat/ai-suggest?peer_id=$ELDER_ID" \
  -H "Authorization: Bearer $HACKER_TOKEN")
BODY=$(echo "$RESP" | head -1)
assert_contains "IDOR: 未绑定用户 AI 建议被拒" "403" "$BODY"

# 绑定用户正常查看 -> 200
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/chat/history?peer_id=$ELDER_ID" \
  -H "Authorization: Bearer $YOUTH_TOKEN")
STATUS=$(echo "$RESP" | tail -1)
assert_status "Chat: 绑定用户正常查看" 200 "$STATUS"
echo ""

# ==================== 10. Weather & News ====================
echo -e "${YELLOW}[10] Weather & News${NC}"

RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/weather?city=武汉" \
  -H "Authorization: Bearer $ELDER_TOKEN")
BODY=$(echo "$RESP" | head -1)
STATUS=$(echo "$RESP" | tail -1)
assert_status "GET /weather" 200 "$STATUS" "$BODY"

RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/weather/care-cards?city=武汉" \
  -H "Authorization: Bearer $ELDER_TOKEN")
STATUS=$(echo "$RESP" | tail -1)
assert_status "GET /weather/care-cards" 200 "$STATUS"

RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/news?city=武汉" \
  -H "Authorization: Bearer $ELDER_TOKEN")
STATUS=$(echo "$RESP" | tail -1)
assert_status "GET /news" 200 "$STATUS"
echo ""

# ==================== 11. JWT 安全 ====================
echo -e "${YELLOW}[11] Security: JWT & Auth${NC}"

# 无 Token 应401
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/user/profile")
STATUS=$(echo "$RESP" | tail -1)
assert_status "No token -> 401" 401 "$STATUS"

# 伪造 Token 应401
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/user/profile" \
  -H "Authorization: Bearer fake-token-attack")
STATUS=$(echo "$RESP" | tail -1)
assert_status "Fake token -> 401" 401 "$STATUS"

# 非法 peer_id 应400
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/chat/history?peer_id=abc" \
  -H "Authorization: Bearer $YOUTH_TOKEN")
STATUS=$(echo "$RESP" | tail -1)
assert_status "Invalid peer_id -> 400" 400 "$STATUS"

# SMS 频控: 60秒内二次发送应受限
RESP=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/v1/auth/sms-code" \
  -H "Content-Type: application/json" \
  -d "{\"phone\":\"$ELDER_PHONE\"}")
BODY=$(echo "$RESP" | head -1)
assert_contains "SMS rate limit" "1008" "$BODY"
echo ""

# ==================== Summary ====================
echo "=========================================="
TOTAL=$((PASSED+FAILED))
echo -e "  Total: $TOTAL | ${GREEN}Passed: $PASSED${NC} | ${RED}Failed: $FAILED${NC}"
echo "=========================================="
if [ "$FAILED" -gt 0 ]; then
    exit 1
fi
