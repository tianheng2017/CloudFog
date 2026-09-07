<script setup lang="ts">
// 公开门户布局（SSR）：顶栏 + 页脚，SEO 页使用 useSeoMeta 独立标题
const nav = computed(() => [
  { to: '/', label: '首页' },
  { to: '/models', label: '模型广场' },
  { to: '/announcements', label: '公告' },
  { to: '/docs', label: '文档' },
])
</script>

<template>
  <div class="cf-site">
    <header class="cf-header">
      <div class="cf-wrap cf-header__inner">
        <NuxtLink to="/" class="cf-brand">
          <svg class="cf-brand__mark" viewBox="0 0 20 20" width="22" height="22" fill="none" aria-hidden="true"><path d="M5 13a3.5 3.5 0 0 1 .6-6.9 4.6 4.6 0 0 1 8.8 1A3.2 3.2 0 0 1 15 13z" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /><path d="M3 16h14M6 16.5c.8-1 2-1.6 3.5-1.6s2.7.6 3.5 1.6" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
          云之雾 <span class="cf-brand__en">CloudFog</span>
        </NuxtLink>
        <nav class="cf-nav">
          <NuxtLink v-for="n in nav" :key="n.to" :to="n.to" class="cf-nav__link" active-class="cf-nav__link--active">{{ n.label }}</NuxtLink>
        </nav>
        <div class="cf-header__actions">
          <NuxtLink to="/console" class="cf-btn cf-btn--ghost">控制台</NuxtLink>
        </div>
      </div>
    </header>

    <main class="cf-main">
      <slot />
    </main>

    <footer class="cf-footer">
      <div class="cf-wrap cf-footer__inner">
        <span>© {{ new Date().getFullYear() }} CloudFog · 云之雾</span>
        <span class="cf-footer__sub">让模型触手可及</span>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.cf-site { min-height: 100vh; display: flex; flex-direction: column; }
.cf-wrap { width: min(1120px, 100% - 48px); margin-inline: auto; }
.cf-header { position: sticky; top: 0; z-index: 20; backdrop-filter: blur(12px); background: color-mix(in srgb, var(--cf-bg) 78%, transparent); border-bottom: 1px solid var(--cf-line); }
.cf-header__inner { height: 56px; display: flex; align-items: center; gap: 24px; }
.cf-brand { display: flex; align-items: baseline; gap: 6px; font-weight: 700; font-size: 17px; color: var(--cf-text); }
.cf-brand__mark { color: var(--cf-brand-500); }
.cf-brand__en { font-size: 12px; font-weight: 500; color: var(--cf-text-tertiary); }
.cf-nav { display: flex; gap: 4px; margin-left: 8px; }
.cf-nav__link { padding: 6px 12px; border-radius: 8px; font-size: 14px; color: var(--cf-text-secondary); }
.cf-nav__link:hover { color: var(--cf-text); background: var(--cf-surface-hover); text-decoration: none; }
.cf-nav__link--active { color: var(--cf-brand-500); background: color-mix(in srgb, var(--cf-brand-500) 10%, transparent); }
.cf-header__actions { margin-left: auto; }
.cf-main { flex: 1; }
.cf-btn { display: inline-flex; align-items: center; justify-content: center; height: 36px; padding: 0 16px; border-radius: 8px; font-size: 14px; cursor: pointer; border: 1px solid transparent; }
.cf-btn--ghost { color: var(--cf-text); border-color: var(--cf-line-strong); background: var(--cf-surface); }
.cf-btn--ghost:hover { border-color: var(--cf-brand-500); color: var(--cf-brand-500); text-decoration: none; }
.cf-footer { border-top: 1px solid var(--cf-line); padding: 28px 0 40px; margin-top: 64px; color: var(--cf-text-tertiary); font-size: 13px; }
.cf-footer__inner { display: flex; justify-content: space-between; }

/* 窄屏门户顶栏：压缩间距/字号，维持 4 导航入口（首行不换行） */
@media (width < 720px) {
  .cf-wrap { width: min(1120px, 100% - 28px); }
  .cf-header__inner { height: 52px; gap: 10px; }
  .cf-brand { font-size: 15px; }
  .cf-brand__en { display: none; }
  .cf-nav { gap: 0; margin-left: 0; }
  .cf-nav__link { padding: 6px 7px; font-size: 13px; }
  .cf-btn { height: 32px; padding: 0 10px; font-size: 13px; }
  .cf-footer__inner { flex-direction: column; gap: 6px; }
}
</style>
