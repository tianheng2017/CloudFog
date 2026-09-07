<script setup lang="ts">
// 公告页（公开 SSR）。数据源后端 /api/v1/public/announcements（b45），未就绪空态。
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
</script>

<template>
  <div class="wrap">
    <header class="pg-head"><h1>公告</h1><p class="pg-sub">服务动态与重要通知</p></header>

    <div v-if="list.length" class="feed">
      <article v-for="a in list" :key="a.id ?? a.title" class="item">
        <h2>{{ a.title }}</h2>
        <p class="item__time num" v-if="a.created_at">{{ a.created_at }}</p>
        <p class="item__body">{{ a.content }}</p>
      </article>
    </div>
    <div v-else class="empty">暂无公告</div>
  </div>
</template>

<style scoped>
.wrap { width: min(820px, 100% - 48px); margin-inline: auto; padding: 40px 0; }
.pg-head h1 { font-size: 30px; font-weight: 800; }
.pg-sub { margin-top: 6px; color: var(--cf-text-secondary); }
.feed { margin-top: 24px; display: flex; flex-direction: column; gap: 12px; }
.item { border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: 12px; padding: 18px 20px; }
.item h2 { font-size: 16px; }
.item__time { margin-top: 2px; font-size: 12px; color: var(--cf-text-tertiary); }
.item__body { margin-top: 10px; font-size: 14px; color: var(--cf-text-secondary); white-space: pre-line; }
.empty { margin-top: 28px; padding: 56px 24px; text-align: center; border: 1px dashed var(--cf-line-strong); border-radius: 14px; color: var(--cf-text-tertiary); }
</style>
