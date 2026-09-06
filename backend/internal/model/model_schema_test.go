package model

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// 编译期/解析期守卫：gorm 能解析每个模型的全部 tag（复合索引、主键、jsonb serializer 等），
// 且 TableName 与 schema 推断的表名一致。若模型 tag 写坏（如非法 priority、重复列名），
// 此测试在 CI 即失败，避免漂移到"模型↔表结构一致性测试"（12 §5.1）才暴露。
func Test_AllModels_ParseAndTableName(t *testing.T) {
	models := []struct {
		m     any
		table string
	}{
		{User{}, "users"},
		{UserBalance{}, "user_balances"},
		{APIKey{}, "api_keys"},
		{Group{}, "groups"},
		{UserAllowedGroup{}, "user_allowed_groups"},
		{Provider{}, "providers"},
		{Channel{}, "channels"},
		{ChannelGroup{}, "channel_groups"},
		{Proxy{}, "proxies"},
		{Model{}, "models"},
		{ModelPrice{}, "model_prices"},
		{ModelMapping{}, "model_mappings"},
		{UsageLog{}, "usage_logs"},
		{UsageDailyStat{}, "usage_daily_stats"},
		{BillingLedger{}, "billing_ledger"},
		{SubscriptionPlan{}, "subscription_plans"},
		{Subscription{}, "subscriptions"},
		{PaymentProvider{}, "payment_providers"},
		{PaymentOrder{}, "payment_orders"},
		{IdempotencyRecord{}, "idempotency_records"},
		{AuditLog{}, "audit_logs"},
		{Setting{}, "settings"},
	}

	var cache sync.Map
	for _, mc := range models {
		s, err := schema.Parse(mc.m, &cache, schema.NamingStrategy{})
		if err != nil {
			t.Errorf("schema.Parse(%T) 失败: %v", mc.m, err)
			continue
		}
		if s.Table != mc.table {
			t.Errorf("schema.Table(%T) = %q, want %q", mc.m, s.Table, mc.table)
		}
		if s.PrioritizedPrimaryField == nil && len(s.PrimaryFields) == 0 {
			t.Errorf("%T 缺少主键定义", mc.m)
		}
	}
}
