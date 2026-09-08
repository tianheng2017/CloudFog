<script setup lang="ts">
// 登录页（CSR）：后端会话 Cookie 由浏览器同源写入（HttpOnly），token 不落 localStorage
definePageMeta({ layout: 'blank' })
const route = useRoute()
const store = useUserStore()
const form = reactive({ login: '', password: '' })
const loading = ref(false)
const errorMsg = ref('')

// 站内安全跳转：仅接受当前源的绝对地址。
// 注意不能只判 `//` —— 浏览器会把 `/\evil.com` 规范化为 `//evil.com`（开放重定向），
// 因此统一用 URL 解析后比对 origin，并显式拒绝含反斜杠/控制字符的值。
function safeRedirect(): string {
  const q = typeof route.query.redirect === 'string' ? route.query.redirect : ''
  if (!q || q.includes('\\') || /[\t\r\n]/.test(q)) return '/console'
  try {
    const u = new URL(q, window.location.origin)
    if (u.origin !== window.location.origin) return '/console'
    return u.pathname + u.search + u.hash
  } catch {
    return '/console'
  }
}

async function submit() {
  errorMsg.value = ''
  if (!form.login || !form.password) { errorMsg.value = '请输入账号与密码'; return }
  loading.value = true
  try {
    await $fetch('/api/v1/auth/login', {
      method: 'POST', credentials: 'include',
      body: { login: form.login, password: form.password },
    })
    await store.fetchMe()
    await navigateTo(safeRedirect())
  } catch (e: any) {
    errorMsg.value = apiMessage(e, '登录失败，请重试')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login">
    <div class="login__brand"><span class="mark">☁</span> 云之雾</div>
    <h1 class="login__title">登录控制台</h1>
    <el-form label-position="top" size="large" @submit.prevent="submit">
      <el-form-item label="用户名 / 邮箱">
        <el-input v-model="form.login" placeholder="username 或 email" autocomplete="username" />
      </el-form-item>
      <el-form-item label="密码">
        <el-input v-model="form.password" type="password" placeholder="••••••••" autocomplete="current-password" show-password @keyup.enter="submit" />
      </el-form-item>
      <el-alert v-if="errorMsg" :title="errorMsg" type="error" :closable="false" style="margin-bottom: 12px" />
      <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">登 录</el-button>
    </el-form>
    <p class="login__alt">还没有账号？<NuxtLink to="/register">立即注册</NuxtLink></p>
  </div>
</template>

<style scoped>
.login { width: min(380px, 100% - 40px); }
.login__brand { font-size: 18px; font-weight: 700; margin-bottom: 28px; display: flex; align-items: center; gap: 6px; }
.mark { filter: drop-shadow(0 0 8px color-mix(in srgb, var(--cf-brand-500) 60%, transparent)); }
.login__title { font-size: 24px; margin-bottom: 20px; }
.login__alt { margin-top: 16px; font-size: 13px; color: var(--cf-text-tertiary); text-align: center; }
</style>
