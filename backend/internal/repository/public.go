package repository

import (
	"context"
)

// PublicAnnouncement 公开公告（07 §3.1；announcements 表随内容批次建，当前无表→列表恒空）。
type PublicAnnouncement struct {
	ID        int64
	Title     string
	Content   string
	CreatedAt string
}

// PublicModelOut 公开模型（含当前生效基础价，07 §1 公开目录；价格文本可空=未定价）。
type PublicModelOut struct {
	ID            int64
	Name          string
	ProviderCode  string
	DisplayName   string
	ContextWindow int
	InputPer1K    *string `gorm:"column:input_price_per_1k"`
	OutputPer1K   *string `gorm:"column:output_price_per_1k"`
	Currency      *string `gorm:"column:currency"`
}

// PublicModels 门户目录：status=active 且其供应商 active 的模型 + 当前生效价
//（lateral 取 effective_from ≤ now 且未过期的最新价；软删两侧均过滤）。
// 与前端 usePublicModels 契约一致（字段名对齐，b4-5）。
func (r *Repository) PublicModels(ctx context.Context) ([]PublicModelOut, error) {
	const q = `
SELECT m.id, m.name, m.provider_code, m.display_name, m.context_window,
       p.input_price_per_1k::text AS input_price_per_1k,
       p.output_price_per_1k::text AS output_price_per_1k,
       p.currency
FROM models m
LEFT JOIN LATERAL (
    SELECT mp.input_price_per_1k, mp.output_price_per_1k, mp.currency
    FROM model_prices mp
    WHERE mp.model_id = m.id AND mp.effective_from <= now()
      AND (mp.effective_to IS NULL OR mp.effective_to > now())
      AND mp.deleted_at IS NULL
    ORDER BY mp.effective_from DESC
    LIMIT 1
) p ON true
JOIN providers pv ON pv.code = m.provider_code AND pv.status = 'active' AND pv.deleted_at IS NULL
WHERE m.status = 'active' AND m.deleted_at IS NULL
ORDER BY m.id`
	var out []PublicModelOut
	if err := r.db.WithContext(ctx).Raw(q).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// PublicAnnouncements 公开公告占位：统一返回空列表——前端空态/SEO 结构已就绪。
func (r *Repository) PublicAnnouncements(ctx context.Context) ([]PublicAnnouncement, error) {
	return []PublicAnnouncement{}, nil
}
