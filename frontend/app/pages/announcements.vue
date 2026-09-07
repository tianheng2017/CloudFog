<script setup lang="ts">
// 公告页（公开 SSR）。数据源后端 /api/v1/public/announcements（b45），未就绪空态。v2
definePageMeta({ layout: 'default' })
usePageSeo({
  title: '公告 — 云之雾',
  description: '云之雾服务公告：新模型上线、价格调整、维护通知。',
})

interface Announcement {
  id?: number
  title?: string
  content?: string
  created_at?: string
}
const { data: list } = await useAsyncData<Announcement[]>('public-announcements', () =>
  $fetch<Announcement[]>(apiUrl('/v1/public/announcements')).catch(() => []),
  { default: () => [] },
)
function dateOf(s?: string) {
  if (!s) return { y: '', md: '' }
  const d = new Date(s)
  const pad = (n: number) => String(n).padStart(2, '0')
  return { y: String(d.getFullYear()), md: `${pad(d.getMonth() + 1)}.${pad(d.getDate())}` }
}
</script>

<template>
  <div class="cf-page cf-page--narrow">
    <header class="cf-page-head">
      <div>
        <span class="cf-eyebrow">Changelog &amp; Notice</span>
        <h1 class="cf-h1">公告</h1>
        <p class="cf-sub">新模型上线、价格调整、维护窗口——服务动态统一在此同步。</p>
      </div>
    </header>

    <div v-if="list.length" class="feed">
      <article v-for="(a, idx) in list" :key="a.id ?? a.title" class="item cf-reveal" v-reveal :style="{ '--d': `${Math.min(idx, 6) * 40}ms` }">
        <div class="item__date num">
          <span class="item__date-md">{{ dateOf(a.created_at).md }}</span>
          <span class="item__date-y">{{ dateOf(a.created_at).y }}</span>
        </div>
        <div class="item__main">
          <h2>{{ a.title }}</h2>
          <p class="item__body">{{ a.content }}</p>
        </div>
      </article>
    </div>
    <div v-else class="cf-empty">
      <div class="cf-empty__icon">◈</div>
      <div class="cf-empty__title">暂无公告</div>
      <p class="cf-empty__sub">服务动态发布后将在此展示；日常可用性见各页面的运行状态点</p>
    </div>
  </div>
</template>

<style scoped>
.feed { margin-top: 26px; display: flex; flex-direction: column; gap: 12px; }
.item { display: grid; grid-template-columns: 76px minmax(0, 1fr); gap: 18px; align-items: start; border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: var(--cf-radius-lg); padding: 20px 22px; position: relative; overflow: hidden; transition: border-color 0.16s ease, transform 0.16s ease, box-shadow 0.22s ease; }
.item::before { content: ''; position: absolute; left: 0; top: 16px; bottom: 16px; width: 2px; border-radius: 2px; background: linear-gradient(180deg, var(--cf-brand-500), color-mix(in srgb, var(--cf-accent-500) 70%, var(--cf-brand-500))); opacity: 0.85; }
.item:hover { border-color: color-mix(in srgb, var(--cf-brand-500) 38%, var(--cf-line)); transform: translateY(-2px); box-shadow: var(--cf-shadow-md); }
.item__date { display: flex; flex-direction: column; align-items: center; gap: 1px; padding-top: 1px; }
.item__date-md { font-size: 17px; font-weight: 700; color: var(--cf-text); line-height: 1.2; }
.item__date-y { font-size: 11px; color: var(--cf-text-tertiary); }
.item__main h2 { font-size: 16.5px; font-weight: 700; letter-spacing: -0.01em; }
.item__body { margin-top: 8px; font-size: 14px; line-height: 1.75; color: var(--cf-text-secondary); white-space: pre-line; }

@media (width < 560px) {
  .item { grid-template-columns: 1fr; gap: 8px; }
  .item__date { flex-direction: row; gap: 8px; align-items: baseline; }
}
</style>
