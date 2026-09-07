<script setup lang="ts">
// 公开门户首页（SSR，SEO）。设计语言：工程精密的开发者基础设施（数据即界面/等宽数字/克制雾蓝渐变）。v3
definePageMeta({ layout: 'default' })
usePageSeo({
  title: '云之雾 CloudFog — 统一的大模型 API 网关',
  description: '一套 API 接入 32+ 模型与 8 家供应商，按量计费、自动降级、账单透明。OpenAI 兼容，改 base_url 即用。',
})

const { data: models } = usePublicModels()
const stats = computed(() => {
  const providerCount = new Set((models.value || []).map((m) => m.provider_code).filter(Boolean)).size
  return { models: models.value?.length ?? 0, providers: providerCount }
})

const steps = [
  { no: '01', title: '创建密钥', desc: '控制台一键生成，明文仅展示一次，可随时吊销。', to: '/console' },
  { no: '02', title: '指向网关', desc: '改 base_url 到 /v1，官方 SDK 零改动直接跑。', to: '/docs' },
  { no: '03', title: '按量即用', desc: '价格快照固化每笔调用，余额与账单实时可查。', to: '/console/billing' },
]

const features = [
  { icon: 'M13 2 3 14h7l-1 8 10-12h-7l1-8z', title: '毫秒级故障转移', desc: '渠道异常自动 failover 到备用供应商，请求不感知，降级有响应头可追踪。' },
  { icon: 'M20 12a8 8 0 1 1-2.3-5.7', title: '一套密钥，全模型', desc: 'OpenAI 兼容入口统一所有上游协议差异；模型映射与别名让迁移零改动。' },
  { icon: 'M12 3l7 3v5c0 5-3.5 8-7 10-3.5-2-7-5-7-10V6l7-3zM9 12l2 2 4-4', title: '信封级安全', desc: '上游凭证 AES-GCM 信封加密，日志与接口零明文；API Key 即时吊销。' },
  { icon: 'M4 19V5m5 14V9m5 10V3m5 16v-7', title: '账单逐笔透明', desc: '每次调用价格快照固化，倍率与价目可回溯；今日/本月按北京时间核算。' },
]
function providerBadge(p: string | undefined) {
  return (p || '?').slice(0, 1).toUpperCase()
}
</script>

<template>
  <div>
    <!-- ── Hero ── -->
    <section class="hero">
      <div class="wrap hero__grid">
        <div class="hero__main">
          <div class="pill"><span class="pill__dot" /><span class="num">v1.0</span> · OpenAI 兼容 · 自动故障转移</div>
          <h1 class="hero__title">一套 API，接入<span class="grad">每一家大模型</span></h1>
          <p class="hero__sub">云之雾是面向开发者的统一大模型网关——32 个模型、8 家供应商只需一枚密钥。按量计费、价格快照、余额逐笔可查。</p>
          <div class="hero__cta">
            <NuxtLink to="/console" class="btn btn--primary">进入控制台<svg class="btn__arrow" viewBox="0 0 16 16" width="15" height="15"><path d="M2 8h11M9 3l5 5-5 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></NuxtLink>
            <NuxtLink to="/models" class="btn btn--ghost">浏览模型</NuxtLink>
          </div>
          <dl class="proof">
            <div class="proof__item"><dt class="num">{{ stats.models || '32+' }}</dt><dd>接入模型</dd></div>
            <div class="proof__item"><dt class="num">{{ stats.providers || '8+' }}</dt><dd>上游供应商</dd></div>
            <div class="proof__item"><dt class="num">99.9%</dt><dd>可用性目标</dd></div>
            <div class="proof__item"><dt class="num">1</dt><dd>个 base_url</dd></div>
          </dl>
        </div>

        <!-- 深色终端（明暗两态均为深底，形成 hero 视觉对撞） -->
        <div class="term" aria-hidden="true">
          <div class="term__bar">
            <span class="term__dot term__dot--d" /><span class="term__dot term__dot--w" /><span class="term__dot term__dot--s" />
            <span class="term__path num">~/quickstart.sh — zsh</span>
            <span class="term__ok">healthy</span>
          </div>
          <div class="term__body">
            <pre><code><span class="t-c"># OpenAI 官方 SDK，改一行即可</span>
<span class="t-k">export</span> OPENAI_BASE_URL=https://api.cloudfog.example/v1
<span class="t-k">export</span> OPENAI_API_KEY=sk-cf-<span class="t-s">…你的密钥…</span>

<span class="t-p">curl</span> /v1/chat/completions \
  -H <span class="t-s">"Authorization: Bearer $OPENAI_API_KEY"</span> \
  -d <span class="t-s">'{ "model": "gpt-4o", "messages": [...] }'</span>

<span class="t-c"># → 200 同一把密钥也能调 claude / glm / kimi…</span></code></pre>
          </div>
          <div class="term__foot">
            <span class="term__live"><span class="cf-live" />gateway online</span>
            <span class="term__meta num">p99 328ms · 8 regions</span>
          </div>
        </div>
      </div>
    </section>

    <!-- ── 真实模型条带 ── -->
    <section class="strip-sec" id="models">
      <div class="wrap">
        <div class="cf-reveal" v-reveal>
          <div class="strip__label"><span class="cf-eyebrow">Live Catalog</span>接入即用 · 真实模型目录</div>
          <div class="strip">
            <span v-for="m in models.slice(0, 14)" :key="m.name" class="chip">
              <i class="chip__ico num">{{ providerBadge(m.provider_code) }}</i><code class="num">{{ m.name }}</code>
            </span>
          </div>
          <NuxtLink to="/models" class="cf-go strip__more">查看全部 {{ models.length || '' }} 个模型
            <svg viewBox="0 0 16 16" width="14" height="14"><path d="M2 8h11M9 3l5 5-5 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
          </NuxtLink>
        </div>
      </div>
    </section>

    <!-- ── 特性 ── -->
    <section class="features">
      <div class="wrap">
        <div class="sec-head cf-reveal" v-reveal><div><span class="cf-eyebrow">Why CloudFog</span><h2>把供应商的复杂度，挡在网关之后</h2></div></div>
        <div class="f-grid cf-reveal" v-reveal :style="{ '--d': '80ms' }">
          <article v-for="f in features" :key="f.title" class="f-card">
            <span class="f-icon"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path :d="f.icon" /></svg></span>
            <h3>{{ f.title }}</h3>
            <p>{{ f.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <!-- ── 价格预览 ── -->
    <section class="preview">
      <div class="wrap">
        <div class="sec-head cf-reveal" v-reveal>
          <div><span class="cf-eyebrow">Pricing</span><h2>价格透明，就像账单本身</h2></div>
          <NuxtLink to="/models" class="cf-go">模型广场<svg viewBox="0 0 16 16" width="14" height="14"><path d="M2 8h11M9 3l5 5-5 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></NuxtLink>
        </div>
        <div v-if="models.length" class="p-grid cf-reveal" v-reveal :style="{ '--d': '80ms' }">
          <article v-for="m in models.slice(0, 6)" :key="m.name" class="p-card">
            <div class="p-card__top">
              <div class="p-card__name">{{ m.display_name || m.name }}</div>
              <span class="p-provider num"><i>{{ providerBadge(m.provider_code) }}</i>{{ m.provider_code }}</span>
            </div>
            <div class="p-card__meta">
              <code class="p-card__slug num">{{ m.name }}</code>
              <span v-if="m.context_window" class="p-card__ctx num">{{ m.context_window.toLocaleString() }} ctx</span>
            </div>
            <div class="p-card__price">
              <div v-if="m.input_price_per_1k" class="p-price num">$ {{ m.input_price_per_1k }}<small>/1k in</small></div>
              <div v-if="m.output_price_per_1k" class="p-price num">$ {{ m.output_price_per_1k }}<small>/1k out</small></div>
              <div v-if="!m.input_price_per_1k && !m.output_price_per_1k" class="p-card__soon">定价即将公布</div>
            </div>
          </article>
        </div>
        <div v-else class="cf-empty">
          <div class="cf-empty__icon">◈</div>
          <div class="cf-empty__title">模型目录正在准备中</div>
          <p class="cf-empty__sub">接入完成后将在此展示全部可用模型与单价</p>
        </div>
      </div>
    </section>

    <!-- ── 三步 ── -->
    <section class="steps">
      <div class="wrap">
        <div class="sec-head cf-reveal" v-reveal><div><span class="cf-eyebrow">Get Started</span><h2>三分钟接入</h2></div></div>
        <div class="s-grid cf-reveal" v-reveal :style="{ '--d': '70ms' }">
          <NuxtLink v-for="s in steps" :key="s.no" :to="s.to" class="s-card">
            <span class="num s-card__no">{{ s.no }}</span>
            <h3>{{ s.title }}</h3>
            <p>{{ s.desc }}</p>
            <span class="s-card__go cf-go">开始<svg viewBox="0 0 16 16" width="13" height="13"><path d="M2 8h11M9 3l5 5-5 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></span>
          </NuxtLink>
        </div>
      </div>
    </section>

    <!-- ── CTA ── -->
    <section class="cta">
      <div class="wrap">
        <div class="cta__inner cf-reveal" v-reveal :style="{ '--d': '40ms' }">
          <div class="cta__glow" aria-hidden="true" />
          <h2>开始你的第一次调用</h2>
          <p>注册即送一套完整控制台：密钥、用量、账单，随时可查可吊销。</p>
          <div class="hero__cta">
            <NuxtLink to="/register" class="btn btn--primary">免费注册<svg class="btn__arrow" viewBox="0 0 16 16" width="15" height="15"><path d="M2 8h11M9 3l5 5-5 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></NuxtLink>
            <NuxtLink to="/docs" class="btn btn--ghost">阅读文档</NuxtLink>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
/* ── 布局基元 ─────────────────────────────── */
.wrap { width: min(1120px, calc(100% - var(--cf-space-6))); margin-inline: auto; }
.sec-head { display: flex; align-items: flex-end; justify-content: space-between; gap: var(--cf-space-4); margin-bottom: 26px; flex-wrap: wrap; }
.sec-head h2 { font-size: clamp(24px, 3.4vw, 34px); font-weight: 800; letter-spacing: -0.022em; max-width: 620px; }
.sec-head .cf-eyebrow { margin-bottom: 8px; }

/* ── Hero ── */
.hero { position: relative; overflow: hidden; padding: clamp(56px, 8vw, 96px) 0 60px; }

.hero::before {
  content: ''; position: absolute; inset: 0; pointer-events: none;
  background:
    radial-gradient(720px 380px at 10% 0%, color-mix(in srgb, var(--cf-brand-500) 20%, transparent), transparent 70%),
    radial-gradient(600px 340px at 96% 4%, color-mix(in srgb, var(--cf-accent-500) 14%, transparent), transparent 70%);
}
.hero__grid { position: relative; display: grid; grid-template-columns: minmax(0, 1.08fr) minmax(0, 0.92fr); gap: clamp(36px, 5vw, 72px); align-items: center; }
.hero__main { animation: rise 0.6s ease both; }
.pill { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; font-weight: 600; color: var(--cf-text-secondary); padding: 6px 13px; border: 1px solid color-mix(in srgb, var(--cf-line-strong) 80%, transparent); border-radius: var(--cf-radius-full); background: color-mix(in srgb, var(--cf-surface) 70%, transparent); backdrop-filter: blur(6px); }
.pill__dot { position: relative; width: 7px; height: 7px; border-radius: 50%; background: var(--cf-semantic-success); box-shadow: 0 0 0 3px color-mix(in srgb, var(--cf-semantic-success) 22%, transparent); }
.pill__dot::after { content: ''; position: absolute; inset: 0; border-radius: 50%; border: 1px solid var(--cf-semantic-success); animation: pill-pulse 2s cubic-bezier(0.2, 0.6, 0.35, 1) infinite; }

@keyframes pill-pulse { 0% { transform: scale(0.7); opacity: 0.9; } 70%, 100% { transform: scale(2.6); opacity: 0; } }
.hero__title { margin-top: 20px; font-size: clamp(38px, 5.4vw, 60px); line-height: 1.1; font-weight: 800; letter-spacing: -0.03em; text-wrap: balance; }
.grad { background: linear-gradient(100deg, var(--cf-brand-400), var(--cf-accent-500)); -webkit-background-clip: text; background-clip: text; color: transparent; }
.hero__sub { margin-top: 18px; max-width: 54ch; font-size: 16.5px; line-height: 1.8; color: var(--cf-text-secondary); }
.hero__cta { margin-top: 30px; display: flex; gap: 12px; flex-wrap: wrap; }
.btn { display: inline-flex; align-items: center; gap: 8px; height: 48px; padding: 0 24px; border-radius: 11px; font-weight: 650; font-size: 15px; border: 1px solid transparent; transition: transform 0.12s ease, box-shadow 0.2s ease, border-color 0.2s ease; }
.btn:hover { transform: translateY(-1px); text-decoration: none; }
.btn--primary { background: linear-gradient(120deg, var(--cf-brand-500), var(--cf-accent-600)); color: var(--cf-neutral-50); box-shadow: var(--cf-shadow-brand); }
.btn--primary:hover { box-shadow: 0 10px 26px color-mix(in srgb, var(--cf-brand-500) 44%, transparent); }
.btn__arrow { transition: transform 0.15s ease; }
.btn--primary:hover .btn__arrow { transform: translateX(2px); }
.btn--ghost { color: var(--cf-text); border-color: var(--cf-line-strong); background: var(--cf-surface); }
.btn--ghost:hover { border-color: var(--cf-brand-500); color: var(--cf-brand-500); }
.proof { margin-top: 42px; display: flex; flex-wrap: wrap; }
.proof__item { padding: 0 24px 0 0; margin-right: 24px; border-right: 1px solid var(--cf-line); }
.proof__item:first-child { padding-left: 0; }
.proof__item:last-child { border-right: 0; margin-right: 0; }
.proof dt { font-size: 25px; font-weight: 800; letter-spacing: -0.02em; line-height: 1; }
.proof dd { margin: 7px 0 0; font-size: 12px; color: var(--cf-text-tertiary); }

/* 深色终端窗口（明暗双态恒定深底） */
.term { animation: rise 0.6s 0.08s ease both; position: relative; overflow: hidden; border-radius: var(--cf-radius-xl); border: 1px solid var(--cf-neutral-800); background: var(--cf-neutral-950); box-shadow: var(--cf-shadow-lg); }
.term::before { content: ''; position: absolute; inset: 0 0 auto; height: 1px; background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--cf-brand-500) 70%, transparent) 30%, color-mix(in srgb, var(--cf-accent-500) 55%, transparent) 70%, transparent); opacity: 0.8; }
.term__bar { display: flex; align-items: center; gap: 8px; padding: 11px 14px; border-bottom: 1px solid var(--cf-neutral-800); }
.term__dot { width: 11px; height: 11px; border-radius: 50%; }
.term__dot--d { background: var(--cf-semantic-danger); }
.term__dot--w { background: var(--cf-semantic-warning); }
.term__dot--s { background: var(--cf-semantic-success); }
.term__path { margin-left: 8px; font-size: 12px; color: var(--cf-neutral-600); text-align: left; }
.term__ok { margin-left: auto; font-size: 11px; color: var(--cf-neutral-600); border: 1px solid var(--cf-neutral-700); border-radius: var(--cf-radius-full); padding: 1px 8px; }
.term__body { padding: 20px 20px 16px; overflow-x: auto; }
.term__body pre { margin: 0; }
.term__body code { font-size: 12.6px; line-height: 1.85; color: var(--cf-neutral-100); }
.t-c { color: var(--cf-neutral-600); }
.t-k { color: var(--cf-brand-400); }
.t-s { color: var(--cf-semantic-success); }
.t-p { color: var(--cf-semantic-info); }
.term__foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 9px 14px; border-top: 1px solid var(--cf-neutral-800); font-size: 11px; color: var(--cf-neutral-500); }
.term__live { display: inline-flex; align-items: center; gap: 7px; color: var(--cf-semantic-success); }
.term__live .cf-live { width: 7px; height: 7px; box-shadow: none; }
.term__meta { text-align: right; }

@keyframes rise { from { opacity: 0; transform: translateY(12px); } to { opacity: 1; transform: none; } }

/* ── 模型条带 ── */
.strip-sec { position: relative; padding: 8px 0 22px; }
.strip__label { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; font-size: 12px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--cf-text-tertiary); }
.strip__label .cf-eyebrow { margin-bottom: 0; }
.strip { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { display: inline-flex; align-items: center; gap: 9px; padding: 5px 13px 5px 6px; border: 1px solid var(--cf-line); border-radius: var(--cf-radius-md); background: var(--cf-surface); font-size: 12.5px; transition: transform 0.14s ease, border-color 0.16s ease, box-shadow 0.2s ease; }
.chip:hover { transform: translateY(-2px); border-color: color-mix(in srgb, var(--cf-brand-500) 45%, var(--cf-line)); box-shadow: var(--cf-shadow-sm); }
.chip__ico { display: grid; place-items: center; width: 21px; height: 21px; border-radius: 6px; font-style: normal; font-size: 11px; font-weight: 700; color: var(--cf-brand-500); background: color-mix(in srgb, var(--cf-brand-500) 12%, transparent); border: 1px solid color-mix(in srgb, var(--cf-brand-500) 24%, transparent); }
.chip code { color: var(--cf-text); font-size: 12px; }
.strip__more { margin-top: 16px; }

/* ── 特性 ── */
.features { padding: clamp(48px, 7vw, 84px) 0 12px; }
.f-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(248px, 1fr)); gap: 14px; }
.f-card { position: relative; border: 1px solid var(--cf-line); border-radius: var(--cf-radius-lg); padding: 22px 20px 24px; background: var(--cf-surface); transition: border-color 0.16s ease, transform 0.16s ease, box-shadow 0.22s ease; }
.f-card::before { content: ''; position: absolute; top: 0; left: 18px; right: 18px; height: 1px; background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--cf-brand-500) 55%, transparent), transparent); opacity: 0; transition: opacity 0.2s ease; }
.f-card:hover { border-color: color-mix(in srgb, var(--cf-brand-500) 45%, var(--cf-line)); transform: translateY(-3px); box-shadow: var(--cf-shadow-md); }
.f-card:hover::before { opacity: 1; }
.f-icon { display: grid; place-items: center; width: 42px; height: 42px; margin-bottom: 15px; border-radius: 11px; color: var(--cf-brand-500); background: color-mix(in srgb, var(--cf-brand-500) 10%, var(--cf-surface-hover)); border: 1px solid color-mix(in srgb, var(--cf-brand-500) 22%, transparent); transition: color 0.18s ease, background-color 0.18s ease; }
.f-card:hover .f-icon { color: var(--cf-neutral-50); background: linear-gradient(140deg, var(--cf-brand-500), var(--cf-accent-600)); }
.f-card h3 { font-size: 16px; font-weight: 700; letter-spacing: -0.01em; }
.f-card p { margin-top: 8px; font-size: 13.5px; line-height: 1.7; color: var(--cf-text-secondary); }

/* ── 价格预览 ── */
.preview { padding: clamp(48px, 7vw, 84px) 0 10px; }
.p-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.p-card { border: 1px solid var(--cf-line); border-radius: var(--cf-radius-lg); padding: 18px 20px; background: var(--cf-surface); display: flex; flex-direction: column; gap: 13px; transition: border-color 0.16s ease, transform 0.16s ease, box-shadow 0.22s ease; }
.p-card:hover { border-color: var(--cf-line-strong); transform: translateY(-2px); box-shadow: var(--cf-shadow-md); }
.p-card__top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.p-card__name { font-weight: 700; font-size: 15.5px; letter-spacing: -0.01em; }
.p-provider { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; font-weight: 600; color: var(--cf-text-tertiary); text-transform: uppercase; letter-spacing: 0.06em; }
.p-provider i { display: grid; place-items: center; width: 17px; height: 17px; border-radius: 5px; font-style: normal; font-size: 10px; color: var(--cf-accent-500); background: color-mix(in srgb, var(--cf-accent-500) 11%, transparent); }
.p-card__meta { display: flex; justify-content: space-between; align-items: baseline; gap: 8px; font-size: 12px; color: var(--cf-text-tertiary); }
.p-card__slug { font-size: 11.5px; color: var(--cf-text-tertiary); text-align: left; }
.p-card__price { display: flex; gap: 20px; border-top: 1px dashed var(--cf-line); padding-top: 12px; }
.p-price { font-size: 16px; font-weight: 700; display: inline-flex; align-items: baseline; gap: 5px; }
.p-price small { font-size: 11px; font-weight: 500; color: var(--cf-text-tertiary); }
.p-card__soon { font-size: 13px; color: var(--cf-text-tertiary); }

/* ── 三步 ── */
.steps { padding: clamp(48px, 7vw, 84px) 0 10px; }
.s-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(256px, 1fr)); gap: 14px; }
.s-card { position: relative; border: 1px solid var(--cf-line); border-radius: var(--cf-radius-lg); padding: 24px 22px 20px; background: var(--cf-surface); overflow: hidden; transition: border-color 0.16s ease, transform 0.16s ease, box-shadow 0.22s ease; }
.s-card::after { content: ''; position: absolute; left: 0; right: 0; bottom: 0; height: 2px; background: linear-gradient(90deg, var(--cf-brand-500), var(--cf-accent-600)); opacity: 0; transition: opacity 0.2s ease; }
.s-card:hover { border-color: color-mix(in srgb, var(--cf-brand-500) 45%, var(--cf-line)); transform: translateY(-3px); box-shadow: var(--cf-shadow-md); text-decoration: none; }
.s-card:hover::after { opacity: 1; }
.s-card__no { display: inline-block; font-size: 13px; font-weight: 700; padding: 3px 9px; border-radius: 7px; color: var(--cf-brand-500); background: color-mix(in srgb, var(--cf-brand-500) 10%, transparent); border: 1px solid color-mix(in srgb, var(--cf-brand-500) 20%, transparent); }
.s-card h3 { margin-top: 15px; font-size: 16.5px; color: var(--cf-text); letter-spacing: -0.01em; }
.s-card p { margin-top: 7px; font-size: 13.5px; line-height: 1.7; color: var(--cf-text-secondary); }
.s-card__go { margin-top: 16px; color: var(--cf-brand-500); }

/* ── CTA（渐变描边卡 + 内部辉光） ── */
.cta { padding: clamp(48px, 7vw, 84px) 0 10px; }

.cta__inner { position: relative; overflow: hidden; text-align: center; padding: clamp(48px, 7vw, 84px) 24px; border-radius: 24px; border: 1px solid transparent; background:
  linear-gradient(color-mix(in srgb, var(--cf-bg) 88%, var(--cf-bg-elev)), color-mix(in srgb, var(--cf-bg) 88%, var(--cf-bg-elev))) padding-box,
  linear-gradient(150deg, color-mix(in srgb, var(--cf-brand-500) 62%, transparent), color-mix(in srgb, var(--cf-accent-500) 44%, transparent) 45%, color-mix(in srgb, var(--cf-line-strong) 55%, transparent) 100%) border-box;
  box-shadow: var(--cf-shadow-lg);
}
.cta__glow { position: absolute; left: 50%; top: -160px; transform: translateX(-50%); width: 560px; height: 320px; pointer-events: none; background: radial-gradient(closest-side, color-mix(in srgb, var(--cf-brand-500) 24%, transparent), transparent); }
.cta__inner h2 { position: relative; font-size: clamp(26px, 3.6vw, 38px); font-weight: 800; letter-spacing: -0.024em; }
.cta__inner p { position: relative; margin: 12px auto 28px; max-width: 52ch; color: var(--cf-text-secondary); }
.cta .hero__cta { position: relative; justify-content: center; }

@media (width < 920px) {
  .hero__grid { grid-template-columns: 1fr; gap: 34px; }
  .term { order: 2; }
}

@media (prefers-reduced-motion: reduce) {
  .hero__main, .term { animation: none; }
}
</style>
