<script setup lang="ts">
definePageMeta({ layout: 'admin' })
const store = useUserStore()
if (!store.loaded) await store.fetchMe()
</script>

<template>
  <div>
    <h1 class="pg-title">运营概览</h1>
    <p class="pg-sub">管理后台 · 信息密度优先</p>
    <div class="panel empty-panel">
      <p>渠道 / 用户 / 模型目录管理面板将于后续迭代开放（当前后端 API 已就绪）。</p>
    </div>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { margin-top: 2px; font-size: 13px; color: var(--cf-text-secondary); }
.panel { margin-top: 24px; border: 1px solid var(--cf-line); border-radius: 12px; background: var(--cf-surface); }
.empty-panel { padding: 32px 24px; color: var(--cf-text-secondary); }
</style>
