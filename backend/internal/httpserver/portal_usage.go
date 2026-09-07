package httpserver

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

// paginate 解析 page/page_size → offset/limit（07 §3.1：page 从 1 起，page_size 默认 20 最大 100）。
func paginate(c *gin.Context) (offset, limit int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return (page - 1) * size, size
}

// usageDefaultWindow 未给时间范围时的兜底窗口：避免对分区大表全历史扫描。
const usageDefaultWindow = 30 * 24 * time.Hour

// parseRange 解析 from/to（RFC3339，可选）。两者都缺省 → 默认最近 30 天窗口
// （usage_logs 按月分区，无界扫描代价高且结果不可预期——审计 b35-1 修复）。
func parseRange(c *gin.Context) (*time.Time, *time.Time, bool) {
	parse := func(s string) (*time.Time, bool) {
		if strings.TrimSpace(s) == "" {
			return nil, true
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, false
		}
		tt := t
		return &tt, true
	}
	from, ok1 := parse(c.Query("from"))
	to, ok2 := parse(c.Query("to"))
	if !ok1 || !ok2 {
		return nil, nil, false
	}
	if from == nil && to == nil {
		now := time.Now().UTC()
		start := now.Add(-usageDefaultWindow)
		from, to = &start, &now // 独立指针（同址会令 to 一并被改写为空窗口）
	}
	return from, to, true
}

// usageItem 用量明细脱敏投影（不含 client_ip/user_agent 等内部字段，08 §4.1）。
func usageItem(u model.UsageLog) gin.H {
	dur := any(nil)
	if u.DurationMs != nil {
		dur = *u.DurationMs
	}
	return gin.H{
		"request_id": u.RequestID, "model": u.Model, "provider": u.ProviderCode,
		"stream": u.Stream, "status_code": u.StatusCode, "error_code": u.ErrorCode,
		"input_tokens": u.InputTokens, "output_tokens": u.OutputTokens,
		"cache_read_tokens": u.CacheReadTokens, "cache_write_tokens": u.CacheWriteTokens,
		"total_cost": u.TotalCost.String(), "duration_ms": dur,
		"created_at": u.CreatedAt,
	}
}

func (p *Portal) handleMyUsage(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	from, to, ok := parseRange(c)
	if !ok {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "from/to 需 RFC3339")
		return
	}
	offset, limit := paginate(c)
	rows, total, err := p.Repo.ListUsageLogs(c.Request.Context(), repository.UsageLogQuery{
		UserID: u.ID, From: from, To: to, Offset: offset, Limit: limit,
	})
	if err != nil {
		p.log().Error("portal usage list", "user_id", u.ID, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "用量查询失败")
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, usageItem(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total,
		"page": offset/limit + 1, "page_size": limit})
}

func (p *Portal) handleMyUsageStats(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	from, to, ok := parseRange(c)
	if !ok {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "from/to 需 RFC3339")
		return
	}
	rows, err := p.Repo.UsageAggregate(c.Request.Context(), u.ID, from, to)
	if err != nil {
		p.log().Error("portal usage stats", "user_id", u.ID, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "统计查询失败")
		return
	}
	items := make([]gin.H, 0, len(rows))
	var reqs, inT, outT int64
	cost := decimal.Zero
	for _, r := range rows {
		items = append(items, gin.H{
			"model": r.Model, "requests": r.Requests,
			"input_tokens": r.InputTokens, "output_tokens": r.OutputTokens,
			"cache_read_tokens": r.CacheReadTokens, "cache_write_tokens": r.CacheWriteTokens,
			"total_cost": r.TotalCost.String(),
		})
		reqs += r.Requests
		inT += r.InputTokens
		outT += r.OutputTokens
		cost = cost.Add(r.TotalCost.Decimal)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "summary": gin.H{
		"requests": reqs, "input_tokens": inT, "output_tokens": outT, "total_cost": cost.String(),
	}})
}

func (p *Portal) handleMyBilling(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	offset, limit := paginate(c)
	typeFilter := strings.TrimSpace(c.Query("type"))
	rows, total, err := p.Repo.ListLedgerByUser(c.Request.Context(), u.ID, typeFilter, offset, limit)
	if err != nil {
		p.log().Error("portal billing list", "user_id", u.ID, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "账单查询失败")
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, gin.H{
			"id": r.ID, "type": r.Type, "amount": r.Amount.String(),
			"balance_after": r.BalanceAfter.String(), "description": r.Description,
			"payment_order_id": r.PaymentOrderID, "request_id": r.RequestID,
			"created_at": r.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total,
		"page": offset/limit + 1, "page_size": limit})
}
