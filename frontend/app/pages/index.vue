<script setup lang="ts">
// 公开门户首页（SSR，SEO 收载 F16）。模型/公告数据源后端 public 端点（B4-5 补齐），未就绪时优雅空态。
definePageMeta({ layout: 'default' })
useSeoMeta({
  title: '云之雾 CloudFog — 统一的大模型 API 网关',
  description: '一套 API 接入多模型，按量计费、即开即用。OpenAI 兼容、自动降级、余额透明可查。',
  ogTitle: '云之雾 CloudFog',
  ogDescription: '统一的大模型 API 网关与开发者控制台',
})

// SSR 预取（payload 随首屏下发与 /models 页共享同一 key，避免二次请求）；后端未就绪 → 空态
const { data: models } = usePublicModels()

const features = [
  { icon: '⚡', title: 'OpenAI 兼容', desc: '改 base_url 即接入，官方 SDK 零改造' },
  { icon: '🛡', title: '智能降级', desc: '渠道故障自动切换，服务不中断' },
  { icon: '📊', title: '账单透明', desc: '每次调用可查，余额实时可对' },
]
</script>

<template>
  <div>
    <section class="hero">
      <div class="wrap">
        <h1 class="hero__title">让每一个模型<br><span class="grad">触手可及</span></h1>
        <p class="hero__sub">云之雾是统一的大模型 API 网关——一套密钥接入多家模型，按量计费、自动降级、用量与账单全程透明。</p>
        <div class="hero__cta">
          <NuxtLink to="/console" class="btn btn--primary">进入控制台</NuxtLink>
          <NuxtLink to="/models" class="btn btn--ghost">浏览模型广场</NuxtLink>
        </div>
        <div class="hero__snippet">
          <code>base_url = https://api.cloudfog.example/v1</code>
        </div>
      </div>
    </section>

    <section class="features">
      <div class="wrap grid3">
        <article v-for="f in features" :key="f.title" class="card">
          <div class="card__icon">{{ f.icon }}</div>
          <h3>{{ f.title }}</h3>
          <p>{{ f.desc }}</p>
        </article>
      </div>
    </section>

    <section id="models" class="models">
      <div class="wrap">
        <div class="sec-head"><h2>模型广场</h2><NuxtLink to="/models" class="sec-more">全部模型 →</NuxtLink></div>
        <div v-if="models.length" class="grid3">
          <article v-for="m in models" :key="m.name" class="card model">
            <div class="model__name">{{ m.display_name || m.name }}</div>
            <div class="model__meta">
              <span>{{ m.provider_code }}</span>
              <span class="num" v-if="m.context_window">{{ m.context_window.toLocaleString() }} ctx</span>
            </div>
          </article>
        </div>
        <div v-else class="empty">模型目录正在准备中，敬请期待</div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.wrap { width: min(1120px, 100% - 48px); margin-inline: auto; }
.hero { padding: 88px 0 72px; position: relative; overflow: hidden; }
.hero::before { content: ''; position: absolute; inset: -40% -20% auto; height: 120%; background: radial-gradient(900px 420px at 20% 0%, color-mix(in srgb, var(--cf-brand-500) 22%, transparent), transparent 70%), radial-gradient(700px 360px at 85% 8%, color-mix(in srgb, var(--cf-accent-500) 14%, transparent), transparent 70%); pointer-events: none; }
.hero .wrap { position: relative; }
.hero__title { font-size: clamp(40px, 6vw, 64px); line-height: 1.14; font-weight: 800; letter-spacing: -0.02em; }
.grad { background: linear-gradient(100deg, var(--cf-brand-400), var(--cf-accent-500)); -webkit-background-clip: text; background-clip: text; color: transparent; }
.hero__sub { margin-top: 20px; max-width: 560px; font-size: 17px; color: var(--cf-text-secondary); }
.hero__cta { margin-top: 32px; display: flex; gap: 12px; }
.btn { display: inline-flex; align-items: center; height: 44px; padding: 0 22px; border-radius: 10px; font-weight: 600; border: 1px solid transparent; }
.btn--primary { background: linear-gradient(120deg, var(--cf-brand-500), var(--cf-accent-600)); color: var(--cf-text-invert); box-shadow: var(--cf-shadow-brand); }
.btn--primary:hover { filter: brightness(1.05); text-decoration: none; }
.btn--ghost { color: var(--cf-text); border-color: var(--cf-line-strong); }
.btn--ghost:hover { border-color: var(--cf-brand-500); color: var(--cf-brand-500); text-decoration: none; }
.hero__snippet { margin-top: 28px; }
.hero__snippet code { font-size: 13px; color: var(--cf-text-secondary); background: var(--cf-surface); border: 1px solid var(--cf-line); padding: 6px 12px; border-radius: 8px; }
.features { padding: 24px 0 16px; }
.grid3 { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 16px; }
.card { border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: 14px; padding: 20px; transition: border-color .15s, transform .15s; }
.card:hover { border-color: var(--cf-line-strong); transform: translateY(-2px); text-decoration: none; }
.card__icon { font-size: 22px; margin-bottom: 10px; }
.card h3 { font-size: 16px; }
.card p { margin-top: 6px; font-size: 13.5px; color: var(--cf-text-secondary); }
.models { padding: 40px 0; }
.sec-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 16px; }
.sec-head h2 { font-size: 22px; }
.sec-more { font-size: 13px; }
.model__name { font-weight: 600; }
.model__meta { margin-top: 8px; display: flex; justify-content: space-between; gap: 8px; font-size: 12px; color: var(--cf-text-tertiary); }
.empty { border: 1px dashed var(--cf-line-strong); border-radius: 14px; padding: 40px; text-align: center; color: var(--cf-text-tertiary); }
</style>
