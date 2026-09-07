<script setup lang="ts">
definePageMeta({ layout: 'console' })
const store = useUserStore()
if (!store.loaded) await store.fetchMe()
</script>

<template>
  <div>
    <h1 class="pg-title">概览</h1>
    <p class="pg-sub">欢迎回来{{ store.me ? `，${store.me.username}` : '' }}</p>

    <div class="panel empty-panel">
      <div class="empty-panel__icon">🔑</div>
      <h3>第一步：创建你的 API 密钥</h3>
      <p>一个密钥即可访问全部已上线模型，按量计费、随时吊销。</p>
      <el-button type="primary" disabled>创建密钥（即将开放）</el-button>
      <p class="hint">完整密钥管理 · 用量统计 · 账单充值将于后续迭代开放</p>
    </div>
  </div>
</template>

<style scoped>
.pg-title { font-size: 24px; font-weight: 700; }
.pg-sub { margin-top: 4px; color: var(--cf-text-secondary); }
.panel { border: 1px solid var(--cf-line); border-radius: 14px; background: var(--cf-surface); }
.empty-panel { margin-top: 28px; padding: 48px 24px; text-align: center; }
.empty-panel__icon { font-size: 34px; margin-bottom: 12px; }
.empty-panel h3 { font-size: 18px; }
.empty-panel p { margin: 8px auto 20px; max-width: 420px; color: var(--cf-text-secondary); }
.hint { font-size: 12px; color: var(--cf-text-tertiary); }
</style>
