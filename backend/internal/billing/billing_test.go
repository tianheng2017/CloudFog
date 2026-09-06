package billing

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestComputeCost(t *testing.T) {
	p := PriceSnapshot{
		InputPer1K:     decimal.NewFromFloat(0.01),
		OutputPer1K:    decimal.NewFromFloat(0.03),
		RateMultiplier: decimal.NewFromFloat(2.0), // 分组倍率
	}
	got := ComputeCost(p, Tokens{Input: 1000, Output: 1000}, false)
	want := decimal.NewFromFloat(0.08) // (0.01+0.03)*2
	if !got.Equal(want) {
		t.Fatalf("ComputeCost = %s, want %s", got, want)
	}
	// per-request 计费模式追加
	got = ComputeCost(PriceSnapshot{PerRequest: decimal.NewFromFloat(0.5)}, Tokens{}, true)
	if !got.Equal(decimal.NewFromFloat(0.5)) {
		t.Fatalf("per-request 计费错误: %s", got)
	}
}

func TestComputeCostCache(t *testing.T) {
	p := PriceSnapshot{
		InputPer1K:     decimal.NewFromInt(1),
		OutputPer1K:    decimal.NewFromInt(1),
		CacheReadPer1K: decimal.NewFromFloat(0.1),
	}
	got := ComputeCost(p, Tokens{Input: 0, Output: 0, CacheRead: 1000}, false)
	if !got.Equal(decimal.NewFromFloat(0.1)) {
		t.Fatalf("cache-read 计费错误: %s", got)
	}
}
