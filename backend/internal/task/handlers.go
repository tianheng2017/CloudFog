package task

import (
	"fmt"
	"sync"
)

// 全局 handler 注册表：业务模块（service 层）经 RegisterHandler 注入，
// worker 角色经 Handlers() 快照启动消费。跨包解耦——task 不反向依赖业务。
var (
	handlerMu sync.RWMutex
	handlers  = map[TaskType]HandlerFunc{}
)

// RegisterHandler 注册任务处理函数。重复注册不同函数视为编程错误。
func RegisterHandler(ty TaskType, fn HandlerFunc) error {
	if ty == "" || fn == nil {
		return fmt.Errorf("task: 注册 handler 需要非空 type 与函数")
	}
	handlerMu.Lock()
	defer handlerMu.Unlock()
	if prev, ok := handlers[ty]; ok && fmt.Sprintf("%p", prev) != fmt.Sprintf("%p", fn) {
		return fmt.Errorf("task: 任务类型 %q 已有 handler，禁止重复注册", ty)
	}
	handlers[ty] = fn
	return nil
}

// Handlers 返回当前注册表快照（worker 启动/热更新用）。
func Handlers() map[TaskType]HandlerFunc {
	handlerMu.RLock()
	defer handlerMu.RUnlock()
	out := make(map[TaskType]HandlerFunc, len(handlers))
	for k, v := range handlers {
		out[k] = v
	}
	return out
}

// HandlerCount 已注册数量（诊断/日志）。
func HandlerCount() int {
	handlerMu.RLock()
	defer handlerMu.RUnlock()
	return len(handlers)
}
