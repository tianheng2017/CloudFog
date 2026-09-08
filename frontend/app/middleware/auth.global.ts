// 全局路由守卫（docs/14-frontend.md §5.1）：登录态 + 角色。
// console/** 需登录；admin/** 需 admin/super_admin。未满足跳 /login 并带 redirect。
export default defineNuxtRouteMiddleware(async (to) => {
  const store = useUserStore()
  const inConsole = to.path === '/console' || to.path.startsWith('/console/')
  const inAdmin = to.path === '/admin' || to.path.startsWith('/admin/')
  if (!inConsole && !inAdmin) return
  if (to.path === '/login') return

  // loaded 为假首探测；已 loaded 但超过探测有效期则重查（会话可能已在停留期间过期）
  if (!store.loaded || store.isStale()) await store.fetchMe()
  if (!store.isAuthed) {
    return navigateTo({ path: '/login', query: { redirect: to.fullPath } })
  }
  if (inAdmin && store.me && !['admin', 'super_admin'].includes(store.me.role)) {
    return navigateTo('/console')
  }
})
