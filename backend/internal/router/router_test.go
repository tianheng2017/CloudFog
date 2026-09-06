package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

type fakeCat struct {
	models     map[string]*model.Model
	prices     map[int64][]*model.ModelPrice // by model id，测试用已按时间升序
	mappings   map[string][]model.ModelMapping
	candidates []*repository.RouteCandidate
}

func (f *fakeCat) ModelByName(_ context.Context, name string) (*model.Model, error) {
	return f.models[name], nil
}
func (f *fakeCat) MappingRows(_ context.Context, alias string) ([]model.ModelMapping, error) {
	return f.mappings[alias], nil
}
func (f *fakeCat) CurrentModelPrice(_ context.Context, modelID int64, at time.Time) (*model.ModelPrice, error) {
	ps := f.prices[modelID]
	var best *model.ModelPrice
	for _, p := range ps {
		if !p.EffectiveFrom.After(at) {
			best = p
		}
	}
	return best, nil
}
func (f *fakeCat) RouteCandidatesByGroup(context.Context, int64) ([]*repository.RouteCandidate, error) {
	return f.candidates, nil
}

func atTime(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func mkChannel(id int64, priority int, weight int, state string) *model.Channel {
	return &model.Channel{
		ID: id, ProviderID: 1, ProviderCode: "openai", Name: "c", Priority: priority, Weight: weight,
		Status: "active", Schedulable: true, CircuitState: state, Credentials: map[string]any{},
	}
}

func mkPrice(modelID int64, eff time.Time, in, out string) *model.ModelPrice {
	return &model.ModelPrice{ModelID: modelID, EffectiveFrom: eff, Currency: "USD"}
}

func TestResolveModel(t *testing.T) {
	fixed := atTime(2026, 9, 6)
	cat := &fakeCat{
		models: map[string]*model.Model{"gpt-4o": {ID: 1, Name: "gpt-4o"}},
		prices: map[int64][]*model.ModelPrice{1: {
			mkPrice(1, atTime(2026, 8, 1), "0.01", "0.03"),
			mkPrice(1, atTime(2026, 9, 1), "0.02", "0.06"),
		}},
	}
	r := &Router{cat: cat, now: func() time.Time { return fixed }}

	got, err := r.Resolve(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model.ID != 1 || !got.Price.EffectiveFrom.Equal(atTime(2026, 9, 1)) {
		t.Fatalf("应取到最近生效价, got eff=%v", got.Price.EffectiveFrom)
	}
	if _, err := r.Resolve(context.Background(), "nope"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("未知模型应 ErrModelNotFound, got %v", err)
	}

	// 无任何生效价 → Price nil（不报错）
	empty := &fakeCat{models: map[string]*model.Model{"x": {ID: 2, Name: "x"}}}
	r2 := &Router{cat: empty, now: func() time.Time { return fixed }}
	got2, err := r2.Resolve(context.Background(), "x")
	if err != nil || got2.Price != nil {
		t.Fatalf("无生效价应返回 Price=nil, got %+v err=%v", got2, err)
	}
}

func TestPlanAttempts(t *testing.T) {
	cat := &fakeCat{
		mappings: map[string][]model.ModelMapping{
			"gpt-4o": {
				{ChannelID: nil, Alias: "gpt-4o", UpstreamModel: "gpt-4o-global"},
				{ChannelID: ptr(int64(11)), Alias: "gpt-4o", UpstreamModel: "gpt-4o-c11"},
			},
		},
		// 按 repository 返回序（priority 升序）排布；open 熔断渠道须被剔除
		candidates: []*repository.RouteCandidate{
			{Channel: mkChannel(12, 1, 99, "open"), Provider: &model.Provider{ID: 1, Code: "openai"}},     // 熔断 open 不可用
			{Channel: mkChannel(11, 5, 50, "closed"), Provider: &model.Provider{ID: 1, Code: "openai"}},   // 渠道级映射
			{Channel: mkChannel(10, 10, 100, "closed"), Provider: &model.Provider{ID: 1, Code: "openai"}}, // 无渠道级映射 → 全局
		},
	}
	r := &Router{cat: cat, now: func() time.Time { return atTime(2026, 9, 6) }}
	got, err := r.Plan(context.Background(), 1, []string{"gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("应仅含 2 个可用渠道（熔断渠道排除），got %d", len(got))
	}
	if got[0].Candidate.Channel.ID != 11 || got[0].UpstreamModel != "gpt-4o-c11" {
		t.Fatalf("attempt[0] 应按 priority 先到 c11 且命中渠道级映射, got %+v", got[0])
	}
	if got[1].Candidate.Channel.ID != 10 || got[1].UpstreamModel != "gpt-4o-global" {
		t.Fatalf("attempt[1] 应为 c10 且回退全局映射, got %+v", got[1])
	}
}

func TestPlanFallbackChain(t *testing.T) {
	cat := &fakeCat{
		candidates: []*repository.RouteCandidate{
			{Channel: mkChannel(10, 10, 100, "closed"), Provider: &model.Provider{ID: 1, Code: "openai"}},
		},
	}
	r := &Router{cat: cat, now: func() time.Time { return atTime(2026, 9, 6) }}
	got, err := r.Plan(context.Background(), 1, []string{"gpt-4o", "gpt-4o-mini"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("链上两个模型 × 1 渠道 = 2 attempts, got %d", len(got))
	}
	if got[0].ModelName != "gpt-4o" || got[1].ModelName != "gpt-4o-mini" {
		t.Fatalf("降级链应保序, got %s,%s", got[0].ModelName, got[1].ModelName)
	}
	if got[0].UpstreamModel != "gpt-4o" { // 无映射透传
		t.Fatalf("无映射应透传同名, got %s", got[0].UpstreamModel)
	}
}

func ptr(v int64) *int64 { return &v }
