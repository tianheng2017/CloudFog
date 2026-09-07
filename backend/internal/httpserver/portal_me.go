package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/auth"
)

// handleMe 当前用户资料（07 §3.1 GET /me）。
func (p *Portal) handleMe(c *gin.Context) {
	pr, ok := PrincipalOf(c)
	if !ok || pr.User == nil {
		abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "缺少会话身份")
		return
	}
	u := pr.User
	c.JSON(http.StatusOK, gin.H{
		"id": u.ID, "username": u.Username, "email": u.Email,
		"role": u.Role, "status": u.Status, "timezone": u.Timezone,
		"default_group_id": u.DefaultGroupID, "created_at": u.CreatedAt,
	})
}

// handleMeBalance 余额与累计（07 §3.1 GET /me/balance）。无余额行（从未资金动作）视为 0。
func (p *Portal) handleMeBalance(c *gin.Context) {
	pr, ok := PrincipalOf(c)
	if !ok || pr.User == nil {
		abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "缺少会话身份")
		return
	}
	bal, err := p.Repo.BalanceByUserID(c.Request.Context(), pr.User.ID)
	if err != nil {
		p.log().Error("portal me balance", "user_id", pr.User.ID, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "余额查询失败")
		return
	}
	balance, recharged, consumed, frozen := "0", "0", "0", "0"
	if bal != nil {
		balance = bal.Balance.String()
		frozen = bal.Frozen.String()
		recharged = bal.TotalRecharged.String()
		consumed = bal.TotalConsumed.String()
	}
	c.JSON(http.StatusOK, gin.H{
		"balance": balance, "frozen": frozen,
		"total_recharged": recharged, "total_consumed": consumed,
	})
}

// handleMeGroups 当前用户可用分组列表（02 §3.4，Key 创建分组选择来源，07 §3.0.1）。
func (p *Portal) handleMeGroups(c *gin.Context) {
	pr, ok := PrincipalOf(c)
	if !ok || pr.User == nil {
		abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "缺少会话身份")
		return
	}
	gs, err := p.Repo.UserAllowedGroups(c.Request.Context(), pr.User.ID)
	if err != nil {
		p.log().Error("portal me groups", "user_id", pr.User.ID, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "分组查询失败")
		return
	}
	def := pr.User.DefaultGroupID
	items := make([]gin.H, 0, len(gs)+1)
	seen := map[int64]bool{}
	for _, g := range gs {
		seen[g.ID] = true
		items = append(items, gin.H{"id": g.ID, "name": g.Name, "status": g.Status,
			"is_default": def != nil && g.ID == *def})
	}
	// 默认分组即使未出现在可见集合（管理建用户场景）也应展示且可创建 Key（服务端 usable 判定含 default）
	if def != nil && !seen[*def] {
		dg, err := p.Repo.GroupByID(c.Request.Context(), *def)
		if err != nil {
			p.log().Error("portal me groups default", "error", err)
			writeAPIError(c, http.StatusInternalServerError, "server_error", "分组查询失败")
			return
		}
		if dg != nil && dg.Status == "active" {
			items = append(items, gin.H{"id": dg.ID, "name": dg.Name, "status": dg.Status, "is_default": true})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
