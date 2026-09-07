<script setup lang="ts">
// 登录页（CSR）：后端会话 Cookie 由浏览器同源写入（HttpOnly），token 不落 localStorage
definePageMeta({ layout: 'blank' })
const route = useRoute()
const store = useUserStore()
const form = reactive({ login: '', password: '' })
const loading = ref(false)
const errorMsg = ref('')

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
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/console'
    await navigateTo(redirect)
  } catch (e: any) {
    errorMsg.value = e?.data?.error?.message || '登录失败，请重试'
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
    <p class="login__alt">还没有账号？自助注册将在后续开放</p>
  </div>
</template>

<style scoped>
.login { width: min(380px, 100% - 40px); }
.login__brand { font-size: 18px; font-weight: 700; margin-bottom: 28px; display: flex; align-items: center; gap: 6px; }
.mark { filter: drop-shadow(0 0 8px color-mix(in srgb, var(--cf-brand-500) 60%, transparent)); }
.login__title { font-size: 24px; margin-bottom: 20px; }
.login__alt { margin-top: 16px; font-size: 13px; color: var(--cf-text-tertiary); text-align: center; }
</style>
