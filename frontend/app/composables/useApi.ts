// 统一 API 调用与取数（2026-09-08 前端优化）：
// 1) 401 会话失效统一处理——此前各页 catch 后静默空态，页面停留期间会话过期不会跳登录，
//    必须手动刷新才被 auth.global 拦截；
// 2) 收敛「loading + try/catch + 空态」取数模板（7 个页面重复），统一给出可重试的错误态；
// 3) 统一错误文案提取（后端 OpenAI error shape / portal 错误 shape 兼容）。
import type { Ref } from 'vue'

export function apiMessage(e: any, fallback = '请求失败'): string {
  const msg = e?.data?.error?.message ?? e?.data?.message ?? e?.message
  return typeof msg === 'string' && msg.trim() ? msg : fallback
}

export function isUnauthorized(e: any): boolean {
  return e?.statusCode === 401 || e?.response?.status === 401
}

// 会话失效：清空 store 并回登录（带 redirect）；仅在客户端执行，登录页自身的 401 不跳转。
async function handleUnauthorized() {
  const store = useUserStore()
  if (!store.isAuthed) return
  store.reset()
  if (!import.meta.client) return
  const route = useRoute()
  if (route.path === '/login') return
  await navigateTo({ path: '/login', query: { redirect: route.fullPath } })
}

// apiFetch 携带会话 Cookie 的 API 调用（替代页面内裸 $fetch）。
export async function apiFetch<T = any>(url: string, opts: Record<string, any> = {}): Promise<T> {
  try {
    return await $fetch<T>(url, { credentials: 'include', ...opts })
  } catch (e) {
    if (isUnauthorized(e)) void handleUnauthorized()
    throw e
  }
}

// useApiResource 声明式取数：data/loading/error/reload（挂载即加载）。
export function useApiResource<T>(fetcher: () => Promise<T>, initial: T) {
  const data = ref(initial) as Ref<T>
  const loading = ref(false)
  const error = ref('')

  async function reload() {
    loading.value = true
    error.value = ''
    try {
      data.value = await fetcher()
    } catch (e) {
      // 401 已由 apiFetch 统一跳登录，此处不重复提示
      if (!isUnauthorized(e)) error.value = apiMessage(e, '加载失败')
    } finally {
      loading.value = false
    }
  }

  onMounted(reload)
  return { data, loading, error, reload }
}
