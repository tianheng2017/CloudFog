package repository_test

// 编译期契约锁定：*Repository 必须满足消费方接口，任何方法签名漂移在此立即失败
// （auth.Lookup：b2-2 鉴权中间件；router.Catalog：b2-3 路由层）。
import (
	"cloudfog/internal/auth"
	"cloudfog/internal/repository"
	"cloudfog/internal/router"
)

var (
	_ auth.Lookup    = (*repository.Repository)(nil)
	_ router.Catalog = (*repository.Repository)(nil)
)
