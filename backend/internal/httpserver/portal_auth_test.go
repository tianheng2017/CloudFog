package httpserver

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginFailureGuard(t *testing.T) {
	p := &Portal{}
	login := "someone@example.com"
	for i := 0; i < 5; i++ {
		if _, blocked := p.blocked(login); blocked {
			t.Fatalf("第 %d 次不应触发锁定", i+1)
		}
		p.recordFailure(login)
	}
	until, blocked := p.blocked(login)
	if !blocked {
		t.Fatal("5 次失败后应锁定")
	}
	if time.Until(until) < loginLockWindow-time.Second || time.Until(until) > loginLockWindow+time.Second {
		t.Fatalf("锁定窗口应为 15min: %v", time.Until(until))
	}
	// 其它账号不受影响
	if _, b := p.blocked("other@example.com"); b {
		t.Fatal("无关账号不应被锁")
	}
	// 成功登录后清零
	p.clearFailures(login)
	if _, b := p.blocked(login); b {
		t.Fatal("clear 后不应再锁定")
	}
	// 窗口自然过期后可重试
	p.mu.Lock()
	p.ensureFails()[login] = &failBucket{until: time.Now().Add(-time.Second)}
	p.mu.Unlock()
	if _, b := p.blocked(login); b {
		t.Fatal("过期窗口应解除锁定")
	}
}

func TestLoginFailureMapBounded(t *testing.T) {
	p := &Portal{}
	// 灌入超上限条目的零-until 计数（模拟枚举不存在账号）——内存有界（≤上限+余量），不清活跃锁定
	for i := 0; i < loginFailMaxTracked*2; i++ {
		p.recordFailure(fmt.Sprintf("nonexist-%d@x.io", i))
	}
	p.mu.Lock()
	n := len(p.fails)
	p.mu.Unlock()
	if n > loginFailMaxTracked+64 {
		t.Fatalf("失败 map 应有界（≤上限+余量）, got %d 条目", n)
	}
	// 生效中的锁定保留
	live := "live-account"
	for i := 0; i < maxLoginFailures; i++ {
		p.recordFailure(live)
	}
	if _, b := p.blocked(live); !b {
		t.Fatal("活跃锁定应保留")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.fails) > loginFailMaxTracked+64 {
		t.Fatalf("加入活跃锁后仍应有界, got %d", len(p.fails))
	}
}
