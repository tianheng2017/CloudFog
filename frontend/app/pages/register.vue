<script setup lang="ts">
// 自助注册（07 §3.0 /auth/register）：注册完成后跳登录。
// 后端要求：邮箱/用户名小写唯一 + 密码≥8；无可用分组时后端返回 503（引导建组）。
definePageMeta({ layout: 'blank' })
const form = reactive({ username: '', email: '', password: '', confirm: '' })
const loading = ref(false)
const errorMsg = ref('')

async function submit() {
  errorMsg.value = ''
  const u = form.username.trim().toLowerCase()
  const e = form.email.trim().toLowerCase()
  if (!u || !e.includes('@')) { errorMsg.value = '请输入用户名与有效邮箱'; return }
  if (form.password.length < 8) { errorMsg.value = '密码至少 8 位'; return }
  if (form.password !== form.confirm) { errorMsg.value = '两次输入的密码不一致'; return }
  loading.value = true
  try {
    await $fetch('/api/v1/auth/register', {
      method: 'POST', credentials: 'include',
      body: { username: u, email: e, password: form.password },
    })
    ElMessage.success('注册成功，请登录')
    await navigateTo('/login')
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message || '注册失败，请重试'
  } finally { loading.value = false }
}
</script>

<template>
  <div class="reg">
    <div class="reg__brand"><span class="mark">☁</span> 云之雾</div>
    <h1 class="reg__title">注册账号</h1>
    <el-form label-position="top" size="large" @submit.prevent="submit">
      <el-form-item label="用户名">
        <el-input v-model="form.username" placeholder="username" autocomplete="username" />
      </el-form-item>
      <el-form-item label="邮箱">
        <el-input v-model="form.email" placeholder="you@example.com" autocomplete="email" />
      </el-form-item>
      <el-form-item label="密码">
        <el-input v-model="form.password" type="password" placeholder="至少 8 位" autocomplete="new-password" show-password />
      </el-form-item>
      <el-form-item label="确认密码">
        <el-input v-model="form.confirm" type="password" placeholder="再次输入" autocomplete="new-password" show-password @keyup.enter="submit" />
      </el-form-item>
      <el-alert v-if="errorMsg" :title="errorMsg" type="error" :closable="false" style="margin-bottom: 12px" />
      <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">注 册</el-button>
    </el-form>
    <p class="reg__alt">已有账号？<NuxtLink to="/login">去登录</NuxtLink></p>
  </div>
</template>

<style scoped>
.reg { width: min(400px, 100% - 40px); }
.reg__brand { font-size: 18px; font-weight: 700; margin-bottom: 24px; display: flex; align-items: center; gap: 6px; }
.mark { filter: drop-shadow(0 0 8px color-mix(in srgb, var(--cf-brand-500) 60%, transparent)); }
.reg__title { font-size: 24px; margin-bottom: 18px; }
.reg__alt { margin-top: 14px; font-size: 13px; color: var(--cf-text-tertiary); text-align: center; }
</style>
