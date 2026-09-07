<script setup lang="ts">
// 公开门户首页（SSR，SEO）。设计语言：工程精密的开发者基础设施（数据即界面/等宽数字/克制雾蓝渐变）。
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
  { no: '01', title: '创建密钥', desc: '控制台一键生成，明文仅展示一次', to: '/console' },
  { no: '02', title: '指向网关', desc: '改 base_url 到 /v1，SDK 零改动', to: '/docs' },
  { no: '03', title: '按量即用', desc: '余额实时可见，账单逐条可查', to: '/console/billing' },
]

const features = [
  { icon: 'M13 2 3 14h7l-1 8 10-12h-7l1-8z', title: '毫秒级切换', desc: '渠道故障自动 failover 到备用供应商，请求不感知、降级有响应头可追踪。' },
  { icon: 'M20 12a8 8 0 1 1-2.3-5.7', title: '一套密钥，全模型', desc: 'OpenAI 兼容入口统一所有上游协议差异；模型映射与别名让迁移零改动。' },
  { icon: 'M12 3l7 3v5c0 5-3.5 8-7 10-3.5-2-7-5-7-10V6l7-3zM9 12l2 2 4-4', title: '信封级安全', desc: '上游凭证 AES-GCM 信封加密，日志与接口零明文；API Key 可即时吊销。' },
  { icon: 'M4 19V5m5 14V9m5 10V3m5 16v-7', title: '计费一目了然', desc: '每次调用价格快照固化，倍率/价目可回溯；今日与本月按北京时间核算。' },
]
</script>

<template>
  <div>
    <section class="hero">
      <div class="wrap hero__grid">
        <div class="hero__main">
          <div class="pill"><span class="pill__dot" /><span class="num">v1.0</span> · OpenAI 兼容 · 自动故障转移</div>
          <h1 class="hero__title">一套 API，<br />接入<span class="grad">每一家大模型</span></h1>
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

        <div class="term" aria-hidden="true">
          <div class="term__bar">
            <span class="term__dot term__dot--d" /><span class="term__dot term__dot--w" /><span class="term__dot term__dot--s" />
            <span class="term__path num">quickstart.sh</span>
          </div>
          <div class="term__body">
            <pre><code><span class="c"># OpenAI 官方 SDK，改一行即可</span>
<span class="k">export</span> OPENAI_BASE_URL=https://api.cloudfog.example/v1
<span class="k">export</span> OPENAI_API_KEY=sk-cf-<span class="s">…你的密钥…</span>

curl /v1/chat/completions \
  -H <span class="s">"Authorization: Bearer $OPENAI_API_KEY"</span> \
  -d <span class="s">'{ "model": "gpt-4o", "messages": [{ "role": "user", "content": "你好" }] }'</span>

<span class="c"># → 200，同一把密钥也能调 claude-sonnet-4 / glm / kimi …</span></code></pre>
          </div>
        </div>
      </div>
    </section>

    <section class="models-strip">
      <div class="wrap">
        <div class="strip__label">接入即用 · 真实模型目录</div>
        <div class="strip">
          <span v-for="m in models.slice(0, 14)" :key="m.name" class="chip"><code class="num">{{ m.name }}</code><i v-if="m.provider_code" class="chip__src">{{ m.provider_code }}</i></span>
        </div>
        <NuxtLink to="/models" class="strip__more">查看全部 {{ models.length || '' }} 个模型 →</NuxtLink>
      </div>
    </section>

    <section class="features">
      <div class="wrap">
        <div class="sec-head"><span class="kicker">Why CloudFog</span><h2>把供应商的复杂度，挡在网关之后</h2></div>
        <div class="f-grid">
          <article v-for="f in features" :key="f.title" class="f-card">
            <svg class="f-icon" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path :d="f.icon" /></svg>
            <h3>{{ f.title }}</h3>
            <p>{{ f.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="preview">
      <div class="wrap">
        <div class="sec-head"><span class="kicker">Pricing</span><h2>价格透明，就像账单本身</h2><NuxtLink to="/models" class="sec-head__link">模型广场 →</NuxtLink></div>
        <div v-if="models.length" class="p-grid">
          <article v-for="m in models.slice(0, 6)" :key="m.name" class="p-card">
            <div class="p-card__top"><span class="p-card__name">{{ m.display_name || m.name }}</span><span class="chip chip--sm"><code class="num">{{ m.name }}</code></span></div>
            <div class="p-card__meta"><span>{{ m.provider_code }}</span><span v-if="m.context_window" class="num">{{ m.context_window.toLocaleString() }} ctx</span></div>
            <div class="p-card__price">
              <div v-if="m.input_price_per_1k" class="num">$ {{ m.input_price_per_1k }}<small>/1k in</small></div>
              <div v-if="m.output_price_per_1k" class="num">$ {{ m.output_price_per_1k }}<small>/1k out</small></div>
              <div v-else class="p-card__soon">定价即将公布</div>
            </div>
          </article>
        </div>
        <div v-else class="p-empty">模型目录正在准备中</div>
      </div>
    </section>

    <section class="steps">
      <div class="wrap">
        <div class="sec-head"><span class="kicker">Get started</span><h2>三分钟接入</h2></div>
        <div class="s-grid">
          <NuxtLink v-for="s in steps" :key="s.no" :to="s.to" class="s-card">
            <div class="num s-card__no">{{ s.no }}</div>
            <h3>{{ s.title }}</h3>
            <p>{{ s.desc }}</p>
            <span class="s-card__go">开始 →</span>
          </NuxtLink>
        </div>
      </div>
    </section>

    <section class="cta">
      <div class="wrap">
        <div class="cta__inner">
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
.wrap { width: min(1160px, 100% - 48px); margin-inline: auto; }
.sec-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; margin-bottom: 26px; flex-wrap: wrap; }
.kicker { display: block; width: 100%; font-size: 12px; font-weight: 700; letter-spacing: 0.12em; text-transform: uppercase; color: var(--cf-brand-500); margin-bottom: 6px; }
.sec-head h2 { font-size: clamp(24px, 3.4vw, 34px); font-weight: 750; letter-spacing: -0.02em; max-width: 560px; }
.sec-head__link { font-size: 14px; }

/* ── Hero ───────────────────────────────── */
.hero { position: relative; overflow: hidden; padding: 72px 0 64px; }

.hero::before { content: ''; position: absolute; inset: -30% -15% auto; height: 130%; pointer-events: none;
  background:
    radial-gradient(700px 360px at 12% 0%, color-mix(in srgb, var(--cf-brand-500) 22%, transparent), transparent 70%),
    radial-gradient(560px 300px at 90% 6%, color-mix(in srgb, var(--cf-accent-500) 16%, transparent), transparent 70%);
}

.hero::after { content: ''; position: absolute; inset: 0; pointer-events: none; opacity: 0.5;
  background-image: linear-gradient(to right, color-mix(in srgb, var(--cf-line) 55%, transparent) 1px, transparent 1px),
                    linear-gradient(to bottom, color-mix(in srgb, var(--cf-line) 55%, transparent) 1px, transparent 1px);
  background-size: 64px 64px;
  mask-image: radial-gradient(700px 420px at 50% 0%, black, transparent);
}
.hero__grid { position: relative; display: grid; grid-template-columns: 1.1fr 0.9fr; gap: 48px; align-items: center; }
.hero__main { animation: rise .6s ease both; }
.pill { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; font-weight: 600; color: var(--cf-text-secondary); padding: 5px 12px; border: 1px solid var(--cf-line); border-radius: 999px; background: color-mix(in srgb, var(--cf-surface) 72%, transparent); backdrop-filter: blur(6px); }
.pill__dot { width: 7px; height: 7px; border-radius: 50%; background: var(--cf-semantic-success); box-shadow: 0 0 0 3px color-mix(in srgb, var(--cf-semantic-success) 22%, transparent); }
.hero__title { margin-top: 22px; font-size: clamp(38px, 5.6vw, 60px); line-height: 1.12; font-weight: 800; letter-spacing: -0.028em; }
.grad { background: linear-gradient(100deg, var(--cf-brand-400), var(--cf-accent-500)); -webkit-background-clip: text; background-clip: text; color: transparent; }
.hero__sub { margin-top: 18px; max-width: 540px; font-size: 16.5px; line-height: 1.75; color: var(--cf-text-secondary); }
.hero__cta { margin-top: 30px; display: flex; gap: 12px; flex-wrap: wrap; }
.btn { display: inline-flex; align-items: center; gap: 8px; height: 46px; padding: 0 22px; border-radius: 10px; font-weight: 650; font-size: 15px; border: 1px solid transparent; transition: transform .12s ease, box-shadow .2s ease, border-color .2s ease; }
.btn:hover { transform: translateY(-1px); text-decoration: none; }
.btn--primary { background: linear-gradient(120deg, var(--cf-brand-500), var(--cf-accent-600)); color: var(--cf-text-invert); box-shadow: var(--cf-shadow-brand); }
.btn--primary:hover { box-shadow: 0 10px 26px color-mix(in srgb, var(--cf-brand-500) 40%, transparent); }
.btn__arrow { transition: transform .15s ease; }
.btn--primary:hover .btn__arrow { transform: translateX(2px); }
.btn--ghost { color: var(--cf-text); border-color: var(--cf-line-strong); background: var(--cf-surface); }
.btn--ghost:hover { border-color: var(--cf-brand-500); color: var(--cf-brand-500); }
.proof { margin-top: 40px; display: grid; grid-template-columns: repeat(4, auto); gap: 0; width: fit-content; }
.proof__item { padding: 4px 22px 4px 0; border-right: 1px solid var(--cf-line); margin-right: 22px; }
.proof__item:last-child { border-right: 0; margin-right: 0; }
.proof dt { font-size: 24px; font-weight: 800; letter-spacing: -0.02em; }
.proof dd { margin: 2px 0 0; font-size: 12px; color: var(--cf-text-tertiary); }

/* 代码窗口 */
.term { animation: rise .6s .08s ease both; border: 1px solid var(--cf-line-strong); border-radius: 14px; overflow: hidden; background: color-mix(in srgb, var(--cf-surface) 60%, transparent); backdrop-filter: blur(8px); box-shadow: var(--cf-shadow-lg); }
.term__bar { display: flex; align-items: center; gap: 8px; padding: 10px 14px; border-bottom: 1px solid var(--cf-line); }
.term__dot { width: 11px; height: 11px; border-radius: 50%; }
.term__dot--d { background: var(--cf-semantic-danger); }
.term__dot--w { background: var(--cf-semantic-warning); }
.term__dot--s { background: var(--cf-semantic-success); }
.term__path { margin-left: auto; font-size: 12px; color: var(--cf-text-tertiary); }
.term__body { padding: 18px 20px 20px; overflow-x: auto; }
.term__body pre { margin: 0; }
.term__body code { font-size: 13px; line-height: 1.8; color: var(--cf-text); }
.term__body .c { color: var(--cf-text-tertiary); }
.term__body .k { color: var(--cf-brand-400); }
.term__body .s { color: var(--cf-semantic-success); }

@keyframes rise { from { opacity: 0; transform: translateY(10px); } to { opacity: 1; transform: none; } }

/* ── 模型条 ─────────────────────────────── */
.models-strip { padding: 26px 0 10px; }
.strip__label { font-size: 12px; letter-spacing: .1em; text-transform: uppercase; color: var(--cf-text-tertiary); margin-bottom: 12px; }
.strip { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { display: inline-flex; align-items: center; gap: 8px; padding: 5px 12px; border: 1px solid var(--cf-line); border-radius: 8px; background: var(--cf-surface); font-size: 12.5px; }
.chip code { color: var(--cf-text); }
.chip__src { font-style: normal; font-size: 11px; color: var(--cf-text-tertiary); }
.chip--sm { padding: 2px 8px; }
.strip__more { display: inline-block; margin-top: 14px; font-size: 13px; }

/* ── 特性 ───────────────────────────────── */
.features { padding: 56px 0 12px; }
.f-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(255px, 1fr)); gap: 14px; }
.f-card { border: 1px solid var(--cf-line); border-radius: 14px; padding: 24px 22px; background: var(--cf-surface); transition: border-color .15s ease, transform .15s ease, box-shadow .2s ease; }
.f-card:hover { border-color: color-mix(in srgb, var(--cf-brand-500) 55%, var(--cf-line)); transform: translateY(-2px); box-shadow: var(--cf-shadow-md); }
.f-icon { color: var(--cf-brand-500); margin-bottom: 14px; }
.f-card h3 { font-size: 16.5px; font-weight: 700; }
.f-card p { margin-top: 8px; font-size: 13.5px; line-height: 1.7; color: var(--cf-text-secondary); }

/* ── 价格预览 ───────────────────────────── */
.preview { padding: 56px 0 10px; }
.p-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.p-card { border: 1px solid var(--cf-line); border-radius: 14px; padding: 18px 20px; background: var(--cf-surface); display: flex; flex-direction: column; gap: 12px; transition: border-color .15s ease, box-shadow .2s ease; }
.p-card:hover { border-color: var(--cf-line-strong); box-shadow: var(--cf-shadow-sm); }
.p-card__top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.p-card__name { font-weight: 700; font-size: 15.5px; }
.p-card__meta { display: flex; justify-content: space-between; font-size: 12px; color: var(--cf-text-tertiary); }
.p-card__price { display: flex; gap: 18px; border-top: 1px dashed var(--cf-line); padding-top: 12px; }
.p-card__price .num { font-size: 16px; font-weight: 700; text-align: left; }
.p-card__price small { font-size: 11px; font-weight: 500; color: var(--cf-text-tertiary); }
.p-card__soon { font-size: 13px; color: var(--cf-text-tertiary); }
.p-empty { border: 1px dashed var(--cf-line-strong); border-radius: 14px; padding: 44px; text-align: center; color: var(--cf-text-tertiary); }

/* ── 三步 ───────────────────────────────── */
.steps { padding: 56px 0 10px; }
.s-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 14px; }
.s-card { border: 1px solid var(--cf-line); border-radius: 14px; padding: 22px; background: var(--cf-surface); transition: border-color .15s ease, transform .15s ease; }
.s-card:hover { border-color: var(--cf-brand-500); transform: translateY(-2px); text-decoration: none; }
.s-card__no { font-size: 26px; font-weight: 800; color: color-mix(in srgb, var(--cf-brand-500) 80%, transparent); letter-spacing: -0.02em; }
.s-card h3 { margin-top: 14px; font-size: 16px; color: var(--cf-text); }
.s-card p { margin-top: 6px; font-size: 13px; color: var(--cf-text-secondary); }
.s-card__go { display: inline-block; margin-top: 14px; font-size: 13px; font-weight: 600; color: var(--cf-brand-500); }

/* ── CTA ────────────────────────────────── */
.cta { padding: 64px 0 12px; }
.cta__inner { position: relative; overflow: hidden; border: 1px solid var(--cf-line-strong); border-radius: 20px; padding: 56px 32px; text-align: center; background: linear-gradient(150deg, color-mix(in srgb, var(--cf-brand-500) 16%, var(--cf-bg)), color-mix(in srgb, var(--cf-accent-500) 12%, var(--cf-bg))); }
.cta__inner h2 { font-size: clamp(24px, 3.4vw, 34px); font-weight: 800; letter-spacing: -0.02em; }
.cta__inner p { margin: 10px auto 26px; max-width: 480px; color: var(--cf-text-secondary); }
.cta .hero__cta { justify-content: center; }

@media (width <= 920px) {
  .hero__grid { grid-template-columns: 1fr; gap: 36px; }
  .term { order: 2; }
}

@media (prefers-reduced-motion: reduce) { .hero__main, .term { animation: none; } }
</style>
