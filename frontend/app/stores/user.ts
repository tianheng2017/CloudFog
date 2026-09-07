// 会话用户 store（§1.3）：登录态经后端 HttpOnly 会话 Cookie 维持，token 不入 localStorage。
export interface MeUser {
  id: number
  username: string
  email: string
  role: 'user' | 'admin' | 'super_admin'
  default_group_id?: number | null
}

export const useUserStore = defineStore('user', () => {
  const me = ref<MeUser | null>(null)
  const loaded = ref(false)
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
    }
  }
  function reset() {
    me.value = null
  }
  return { me, loaded, isAuthed, fetchMe, reset }
})
