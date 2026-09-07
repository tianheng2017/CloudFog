<script setup lang="ts">
// 管理后台布局：信息密度优先（§11），顶栏 + 左侧分组导航。
// 注：渠道/用户/模型/供应商/审计等面板随 B4-7 落地后挂入（当前不放置死链）。
const groups = [
  { title: '运营', items: [{ to: '/admin', label: '概览' }] },
]
</script>

<template>
  <div class="a-shell">
    <aside class="a-side">
      <NuxtLink to="/admin" class="a-brand">云之雾 Admin</NuxtLink>
      <nav class="a-nav">
        <template v-for="g in groups" :key="g.title">
          <div class="a-nav__title">{{ g.title }}</div>
          <NuxtLink v-for="i in g.items" :key="i.to" :to="i.to" class="a-nav__link" active-class="a-nav__link--active">{{ i.label }}</NuxtLink>
        </template>
      </nav>
      <div class="a-side__foot">
        <NuxtLink to="/" class="a-nav__link">← 门户</NuxtLink>
      </div>
    </aside>
    <section class="a-body">
      <slot />
    </section>
  </div>
</template>

<style scoped>
.a-shell { min-height: 100vh; display: flex; }
.a-side { width: 208px; flex: none; border-right: 1px solid var(--cf-line); background: var(--cf-surface); padding: 16px 12px; display: flex; flex-direction: column; gap: 16px; }
.a-brand { font-weight: 700; padding: 4px 12px 12px; border-bottom: 1px solid var(--cf-line); color: var(--cf-text); }
.a-nav { flex: 1; display: flex; flex-direction: column; gap: 2px; font-size: 13px; }
.a-nav__title { margin: 10px 12px 2px; font-size: 11px; letter-spacing: .06em; text-transform: uppercase; color: var(--cf-text-tertiary); }
.a-nav__link { padding: 6px 12px; border-radius: 6px; color: var(--cf-text-secondary); }
.a-nav__link:hover { background: var(--cf-surface-hover); color: var(--cf-text); text-decoration: none; }
.a-nav__link--active { background: color-mix(in srgb, var(--cf-brand-500) 10%, transparent); color: var(--cf-brand-500); font-weight: 600; }
.a-body { flex: 1; min-width: 0; padding: 20px 24px; background: var(--cf-bg); }
</style>
