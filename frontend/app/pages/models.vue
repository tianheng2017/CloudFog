<script setup lang="ts">
// 模型广场（公开 SSR，SEO 收载 F16）。数据源后端 /api/v1/public/models（b45），未就绪优雅空态。
definePageMeta({ layout: 'default' })
usePageSeo({
  title: '模型广场 — 云之雾',
  description: '浏览云之雾当前开放的模型：上下文窗口、计费单价一目了然。OpenAI 兼容接入。',
})
const { data: models } = usePublicModels()
</script>

<template>
  <div class="wrap">
    <header class="pg-head">
      <h1>模型广场</h1>
      <p class="pg-sub">一套密钥接入多模型，按量计费、价格透明</p>
    </header>

    <div v-if="models.length" class="grid">
      <article v-for="m in models" :key="m.name" class="card">
        <div class="card__row">
          <h3 class="card__name">{{ m.display_name || m.name }}</h3>
          <span class="cf-tag cf-tag--muted">{{ m.provider_code || 'unknown' }}</span>
        </div>
        <div class="card__meta">
          <span v-if="m.context_window" class="num">{{ m.context_window.toLocaleString() }} ctx</span>
          <span v-if="m.input_price_per_1k" class="num">{{ m.input_price_per_1k }} /1k in</span>
          <span v-if="m.output_price_per_1k" class="num">{{ m.output_price_per_1k }} /1k out</span>
        </div>
        <code class="card__slug">{{ m.name }}</code>
      </article>
    </div>
    <div v-else class="empty">
      <p>模型目录正在准备中</p>
      <p class="empty__sub">接入工作完成后将在此展示全部可用模型与单价</p>
    </div>
  </div>
</template>

<style scoped>
.wrap { width: min(1120px, 100% - 48px); margin-inline: auto; padding: 40px 0; }
.pg-head h1 { font-size: 30px; font-weight: 800; letter-spacing: -0.01em; }
.pg-sub { margin-top: 6px; color: var(--cf-text-secondary); }
.grid { margin-top: 28px; display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 16px; }
.card { border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: 14px; padding: 18px 20px; }
.card__row { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.card__name { font-size: 16px; font-weight: 650; }
.card__meta { margin-top: 10px; display: flex; flex-wrap: wrap; gap: 8px 16px; color: var(--cf-text-secondary); font-size: 13px; }
.card__slug { display: inline-block; margin-top: 12px; font-size: 12px; color: var(--cf-text-tertiary); }
.empty { margin-top: 28px; border: 1px dashed var(--cf-line-strong); border-radius: 14px; padding: 56px 24px; text-align: center; color: var(--cf-text-secondary); }
.empty__sub { margin-top: 6px; font-size: 13px; color: var(--cf-text-tertiary); }
</style>
