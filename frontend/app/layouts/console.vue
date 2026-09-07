<script setup lang="ts">
// 用户控制台布局（CSR 单页）：侧栏导航 + 顶栏（主题/登出）
const store = useUserStore()
const nav = [
  { to: '/console', label: '概览' },
  { to: '/console/keys', label: 'API 密钥' },
  { to: '/console/usage', label: '用量' },
  { to: '/console/billing', label: '账单与充值' },
]
const setTheme = inject<(d: boolean) => void>('theme-toggle')
const dark = ref(false)
function toggleTheme() {
  dark.value = !document.documentElement.classList.contains('dark')
  setTheme?.(dark.value)
}
async function logout() {
  try { await $fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'include' }) } catch { /* 忽略 */ }
  store.reset()
  await navigateTo('/login')
}
</script>

<template>
  <div class="c-shell">
    <aside class="c-side">
      <NuxtLink to="/console" class="c-brand">☁ 云之雾</NuxtLink>
      <nav class="c-nav">
        <NuxtLink v-for="n in nav" :key="n.to" :to="n.to" class="c-nav__link" active-class="c-nav__link--active">{{ n.label }}</NuxtLink>
      </nav>
      <div class="c-side__foot">
        <NuxtLink to="/" class="c-nav__link">← 返回门户</NuxtLink>
      </div>
    </aside>
    <section class="c-body">
      <header class="c-top">
        <span class="c-top__user">{{ store.me ? store.me.username : '' }}</span>
        <el-switch :model-value="dark" aria-label="主题" @change="toggleTheme" inline-prompt active-text="暗" inactive-text="亮" />
        <el-button link type="primary" @click="logout">退出登录</el-button>
      </header>
      <slot />
    </section>
  </div>
</template>

<style scoped>
.c-shell { min-height: 100vh; display: flex; }
.c-side { width: 220px; flex: none; border-right: 1px solid var(--cf-line); display: flex; flex-direction: column; padding: 20px 12px; gap: 20px; background: var(--cf-surface); position: sticky; top: 0; height: 100vh; }
.c-brand { font-weight: 700; font-size: 16px; padding: 4px 12px; color: var(--cf-text); }
.c-nav { display: flex; flex-direction: column; gap: 2px; flex: 1; }
.c-nav__link { padding: 8px 12px; border-radius: 8px; color: var(--cf-text-secondary); font-size: 14px; }
.c-nav__link:hover { background: var(--cf-surface-hover); color: var(--cf-text); text-decoration: none; }
.c-nav__link--active { color: var(--cf-brand-500); background: color-mix(in srgb, var(--cf-brand-500) 10%, transparent); font-weight: 600; }
.c-side__foot { border-top: 1px solid var(--cf-line); padding-top: 12px; }
.c-body { flex: 1; min-width: 0; padding: 20px 28px; }
.c-top { display: flex; align-items: center; justify-content: flex-end; gap: 16px; margin-bottom: 20px; }
.c-top__user { font-size: 13px; color: var(--cf-text-secondary); }
</style>
