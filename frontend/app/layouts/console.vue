<script setup lang="ts">
// 用户控制台布局（CSR 单页）：侧栏导航。
// 注：密钥/用量/账单页随 B4-6 落地后挂入（当前不放置死链）。
const nav = [
  { to: '/console', label: '概览' },
]
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
      <slot />
    </section>
  </div>
</template>

<style scoped>
.c-shell { min-height: 100vh; display: flex; }
.c-side { width: 220px; flex: none; border-right: 1px solid var(--cf-line); display: flex; flex-direction: column; padding: 20px 12px; gap: 20px; background: var(--cf-surface); }
.c-brand { font-weight: 700; font-size: 16px; padding: 4px 12px; color: var(--cf-text); }
.c-nav { display: flex; flex-direction: column; gap: 2px; flex: 1; }
.c-nav__link { padding: 8px 12px; border-radius: 8px; color: var(--cf-text-secondary); font-size: 14px; }
.c-nav__link:hover { background: var(--cf-surface-hover); color: var(--cf-text); text-decoration: none; }
.c-nav__link--active { color: var(--cf-brand-500); background: color-mix(in srgb, var(--cf-brand-500) 10%, transparent); font-weight: 600; }
.c-body { flex: 1; min-width: 0; padding: 28px 32px; }
</style>
