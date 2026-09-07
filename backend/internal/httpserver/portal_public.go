package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 公开门户只读端点（07 §1 D 类 /api/v1/public，SSR SEO 数据源；无鉴权）。
// 输出契约与前端 usePublicModels/公告页对齐（b4-5）。

// publicModelItem 公开模型投影（价格可空则不出现对应字段）。
type publicModelItem struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	ProviderCode     string  `json:"provider_code"`
	DisplayName      *string `json:"display_name,omitempty"`
	ContextWindow    int     `json:"context_window"`
	InputPricePer1K  *string `json:"input_price_per_1k,omitempty"`
	OutputPricePer1K *string `json:"output_price_per_1k,omitempty"`
	Currency         *string `json:"currency,omitempty"`
}

func (p *Portal) handlePublicModels(c *gin.Context) {
	rows, err := p.Repo.PublicModels(c.Request.Context())
	if err != nil {
		p.log().Error("public models", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "模型目录暂不可用")
		return
	}
	items := make([]publicModelItem, 0, len(rows))
	for _, r := range rows {
		it := publicModelItem{ID: r.ID, Name: r.Name, ProviderCode: r.ProviderCode,
			ContextWindow: r.ContextWindow}
		if r.DisplayName != "" {
			it.DisplayName = &r.DisplayName
		}
		if r.InputPer1K != nil {
			it.InputPricePer1K = r.InputPer1K
		}
		if r.OutputPer1K != nil {
			it.OutputPricePer1K = r.OutputPer1K
		}
		it.Currency = r.Currency
		items = append(items, it)
	}
	c.JSON(http.StatusOK, items)
}

func (p *Portal) handlePublicAnnouncements(c *gin.Context) {
	list, err := p.Repo.PublicAnnouncements(c.Request.Context())
	if err != nil {
		p.log().Error("public announcements", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "公告暂不可用")
		return
	}
	c.JSON(http.StatusOK, list)
}
