<script setup lang="ts">
// 模型广场（公开 SSR，SEO 收载 F16）。数据源后端 /api/v1/public/models（b45），未就绪优雅空态。v2
definePageMeta({ layout: 'default' })
usePageSeo({
  title: '模型广场 — 云之雾',
  description: '浏览云之雾当前开放的模型：上下文窗口、计费单价一目了然。OpenAI 兼容接入。',
})
const { data: models } = usePublicModels()
const stats = computed(() => {
  const providers = new Set((models.value || []).map((m) => m.provider_code).filter(Boolean)).size
  const ctxs = (models.value || []).map((m) => m.context_window).filter((v): v is number => !!v)
  const maxCtx = ctxs.length ? Math.max(...ctxs) : 0
  return { models: models.value?.length ?? 0, providers, maxCtx }
})
function providerBadge(p: string | undefined) {
  return (p || '?').slice(0, 1).toUpperCase()
}
</script>

<template>
  <div class="cf-page">
    <header class="cf-page-head">
      <div>
        <span class="cf-eyebrow">Model Catalog</span>
        <h1 class="cf-h1">模型广场</h1>
        <p class="cf-sub">一套密钥接入多模型，按量计费、价格透明。上下文窗口与单价全部陈列，选型一目了然。</p>
      </div>
      <div v-if="models.length" class="cf-page-head__aside">
        <span class="stat"><b class="num">{{ stats.models }}</b><i>模型</i></span>
        <span class="stat"><b class="num">{{ stats.providers }}</b><i>供应商</i></span>
        <span v-if="stats.maxCtx" class="stat"><b class="num">{{ stats.maxCtx.toLocaleString() }}</b><i>最大 ctx</i></span>
      </div>
    </header>

    <div v-if="models.length" class="grid">
      <article v-for="(m, idx) in models" :key="m.name" class="card cf-reveal" v-reveal :style="{ '--d': `${Math.min(idx, 8) * 40}ms` }">
        <div class="card__top">
          <span class="provider num"><i>{{ providerBadge(m.provider_code) }}</i>{{ m.provider_code || 'unknown' }}</span>
          <span class="card__ctx num" v-if="m.context_window">{{ m.context_window.toLocaleString() }} ctx</span>
        </div>
        <h2 class="card__name">{{ m.display_name || m.name }}</h2>
        <code class="card__slug num">{{ m.name }}</code>
        <div class="card__price">
          <div v-if="m.input_price_per_1k" class="price num">$ {{ m.input_price_per_1k }}<small>/1k in</small></div>
          <div v-if="m.output_price_per_1k" class="price num">$ {{ m.output_price_per_1k }}<small>/1k out</small></div>
          <div v-if="!m.input_price_per_1k && !m.output_price_per_1k" class="soon">定价即将公布</div>
        </div>
      </article>
    </div>
    <div v-else class="cf-empty">
      <div class="cf-empty__icon">◈</div>
      <div class="cf-empty__title">模型目录正在准备中</div>
      <p class="cf-empty__sub">接入工作完成后将在此展示全部可用模型与单价</p>
    </div>
  </div>
</template>

<style scoped>
.cf-page-head__aside { display: flex; gap: 10px; }
.stat { display: flex; flex-direction: column; align-items: center; min-width: 84px; gap: 2px; padding: 12px 16px; border: 1px solid var(--cf-line); border-radius: var(--cf-radius-lg); background: var(--cf-surface); }
.stat b { font-size: 19px; font-weight: 800; letter-spacing: -0.01em; }
.stat i { font-style: normal; font-size: 11px; color: var(--cf-text-tertiary); }

.grid { margin-top: 28px; display: grid; grid-template-columns: repeat(auto-fill, minmax(292px, 1fr)); gap: 14px; }
.card { position: relative; display: flex; flex-direction: column; gap: 9px; border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: var(--cf-radius-lg); padding: 18px 20px 16px; overflow: hidden; transition: border-color 0.16s ease, transform 0.16s ease, box-shadow 0.22s ease; }
.card::before { content: ''; position: absolute; top: 0; left: 16px; right: 16px; height: 1px; background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--cf-brand-500) 55%, transparent), transparent); opacity: 0; transition: opacity 0.2s ease; }
.card:hover { border-color: color-mix(in srgb, var(--cf-brand-500) 40%, var(--cf-line)); transform: translateY(-3px); box-shadow: var(--cf-shadow-md); }
.card:hover::before { opacity: 1; }
.card__top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.provider { display: inline-flex; align-items: center; gap: 7px; font-size: 11px; font-weight: 600; color: var(--cf-text-tertiary); text-transform: uppercase; letter-spacing: 0.06em; }
.provider i { display: grid; place-items: center; width: 20px; height: 20px; border-radius: 6px; font-style: normal; font-size: 11px; color: var(--cf-accent-500); background: color-mix(in srgb, var(--cf-accent-500) 11%, transparent); border: 1px solid color-mix(in srgb, var(--cf-accent-500) 22%, transparent); }
.card__ctx { font-size: 11.5px; color: var(--cf-text-tertiary); }
.card__name { font-size: 16px; font-weight: 700; letter-spacing: -0.01em; }
.card__slug { display: inline-block; font-size: 11.5px; color: var(--cf-text-tertiary); text-align: left; }
.card__price { display: flex; gap: 18px; border-top: 1px dashed var(--cf-line); margin-top: 3px; padding-top: 11px; }
.price { display: inline-flex; align-items: baseline; gap: 5px; font-size: 15.5px; font-weight: 700; }
.price small { font-size: 11px; font-weight: 500; color: var(--cf-text-tertiary); }
.soon { font-size: 13px; color: var(--cf-text-tertiary); }
</style>
