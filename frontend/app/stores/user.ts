// 会话用户 store（§1.3）：登录态经后端 HttpOnly 会话 Cookie 维持，token 不入 localStorage。
export interface MeUser {
  id: number
  username: string
  email: string
  role: 'user' | 'admin' | 'super_admin'
  default_group_id?: number | null
}

// sessionMaxAgeMs 会话探测有效期：超过则守卫重新拉取 /me，
// 覆盖"页面停留期间会话过期"场景（此前 loaded 一旦为真永不回退）。
const sessionMaxAgeMs = 60_000

export const useUserStore = defineStore('user', () => {
  const me = ref<MeUser | null>(null)
  const loaded = ref(false)
  const lastCheck = ref(0)
  const isAuthed = computed(() => me.value !== null)

  // 由 middleware / 控制台首屏调用，拉取 /api/v1/me（401 → 清空）
  async function fetchMe() {
    try {
      const res = await $fetch<MeUser>('/api/v1/me', { credentials: 'include' })
      me.value = res
    } catch {
      me.value = null
    } finally {
      loaded.value = true
      lastCheck.value = Date.now()
    }
  }
  function isStale() {
    return Date.now() - lastCheck.value > sessionMaxAgeMs
  }
  function reset() {
    me.value = null
    loaded.value = false // 退出后置未加载：下次进受保护页重新探测会话
    lastCheck.value = 0
  }
  return { me, loaded, isAuthed, fetchMe, isStale, reset }
})
