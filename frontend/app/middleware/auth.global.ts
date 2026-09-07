// 全局路由守卫（docs/14-frontend.md §5.1）：登录态 + 角色。
// console/** 需登录；admin/** 需 admin/super_admin。未满足跳 /login 并带 redirect。
export default defineNuxtRouteMiddleware(async (to) => {
  const store = useUserStore()
  const requiresAuth = to.path.startsWith('/console') || to.path.startsWith('/admin')
  if (!requiresAuth || to.path === '/login') return

  if (!store.loaded) await store.fetchMe()
  if (!store.isAuthed) {
    return navigateTo({ path: '/login', query: { redirect: to.fullPath } })
  }
  if (to.path.startsWith('/admin') && store.me && !['admin', 'super_admin'].includes(store.me.role)) {
    return navigateTo('/console')
  }
})
