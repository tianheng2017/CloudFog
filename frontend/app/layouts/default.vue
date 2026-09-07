<script setup lang="ts">
// 公开门户布局（SSR）：氛围背景层 + 顶栏 + 品牌页脚。SEO 页用 useSeoMeta 独立标题
const nav = computed(() => [
  { to: '/', label: '首页' },
  { to: '/models', label: '模型广场' },
  { to: '/docs', label: '文档' },
  { to: '/announcements', label: '公告' },
])
const footerCols = computed(() => [
  { title: '产品', links: [{ to: '/models', label: '模型广场' }, { to: '/docs', label: '文档' }, { to: '/announcements', label: '公告' }] },
  { title: '开发者', links: [{ to: '/console', label: '控制台' }, { to: '/register', label: '免费注册' }, { to: '/docs', label: '快速接入' }] },
])
</script>

<template>
  <div class="cf-site">
    <!-- 全页氛围：顶部雾光 + 细网格，z-0；内容层 z-1 -->
    <div class="cf-ambient" aria-hidden="true">
      <span class="cf-ambient__glow cf-ambient__glow--a" />
      <span class="cf-ambient__glow cf-ambient__glow--b" />
    </div>

    <header class="cf-header">
      <div class="cf-wrap cf-header__inner">
        <NuxtLink to="/" class="cf-brand" aria-label="云之雾 CloudFog 首页">
          <span class="cf-brand__mark">
            <svg viewBox="0 0 20 20" width="20" height="20" fill="none" aria-hidden="true"><path d="M5 13a3.5 3.5 0 0 1 .6-6.9 4.6 4.6 0 0 1 8.8 1A3.2 3.2 0 0 1 15 13z" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /><path d="M3 16h14M6 16.5c.8-1 2-1.6 3.5-1.6s2.7.6 3.5 1.6" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
          </span>
          <span class="cf-brand__word">云之雾</span>
          <span class="cf-brand__en">CloudFog</span>
        </NuxtLink>

        <nav class="cf-nav" aria-label="主导航">
          <NuxtLink v-for="n in nav" :key="n.to" :to="n.to" class="cf-nav__link" active-class="cf-nav__link--active">{{ n.label }}</NuxtLink>
        </nav>

        <div class="cf-header__actions">
          <NuxtLink to="/console" class="cf-btn cf-btn--ghost">登录</NuxtLink>
          <NuxtLink to="/register" class="cf-btn cf-btn--primary">免费开始<svg class="cf-btn__arrow" viewBox="0 0 16 16" width="14" height="14"><path d="M2 8h11M9 3l5 5-5 5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg></NuxtLink>
        </div>
      </div>
    </header>

    <main class="cf-main">
      <slot />
    </main>

    <footer class="cf-footer">
      <div class="cf-wrap cf-footer__grid">
        <div class="cf-footer__brand">
          <span class="cf-brand__word" style="font-weight: 700; font-size: 17px">云之雾 <span class="cf-brand__en">CloudFog</span></span>
          <p class="cf-footer__blurb">面向开发者的统一大模型网关：一套密钥接入多模型，按量计费、价格快照、自动故障转移。</p>
        </div>
        <nav v-for="col in footerCols" :key="col.title" class="cf-footer__col">
          <div class="cf-footer__title">{{ col.title }}</div>
          <NuxtLink v-for="l in col.links" :key="l.label" :to="l.to" class="cf-footer__link">{{ l.label }}</NuxtLink>
        </nav>
      </div>
      <div class="cf-wrap cf-footer__bottom">
        <span>© {{ new Date().getFullYear() }} CloudFog · 云之雾</span>
        <span class="cf-footer__status"><span class="cf-live" aria-hidden="true" />所有系统运行正常</span>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.cf-site { min-height: 100vh; display: flex; flex-direction: column; }

/* ── 全页氛围层（网格全站、顶部雾光只在前两屏，随滚动淡出） ── */
.cf-ambient { position: fixed; inset: 0; z-index: 0; pointer-events: none; overflow: hidden; }

.cf-ambient::before {
  content: ''; position: absolute; inset: 0;
  background-image:
    linear-gradient(to right, color-mix(in srgb, var(--cf-line) 42%, transparent) 1px, transparent 1px),
    linear-gradient(to bottom, color-mix(in srgb, var(--cf-line) 42%, transparent) 1px, transparent 1px);
  background-size: 44px 44px;
  -webkit-mask-image: radial-gradient(720px 420px at 50% -6%, black 30%, transparent 78%);
  mask-image: radial-gradient(720px 420px at 50% -6%, black 30%, transparent 78%);
}
.cf-ambient__glow { position: absolute; border-radius: 50%; filter: blur(60px); opacity: 0.5; }
.cf-ambient__glow--a { width: 560px; height: 420px; left: -160px; top: -140px; background: color-mix(in srgb, var(--cf-brand-500) 26%, transparent); }
.cf-ambient__glow--b { width: 460px; height: 380px; right: -120px; top: -60px; background: color-mix(in srgb, var(--cf-accent-500) 18%, transparent); }

@media (prefers-reduced-motion: reduce) {
  .cf-ambient::before { opacity: 0.6; }
}

/* ── 顶栏 ── */
.cf-header { position: sticky; top: 0; z-index: 30; backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px); background: color-mix(in srgb, var(--cf-bg) 74%, transparent); border-bottom: 1px solid color-mix(in srgb, var(--cf-line) 72%, transparent); }
.cf-wrap { width: min(1120px, calc(100% - var(--cf-space-6))); margin-inline: auto; }
.cf-header__inner { height: 64px; display: flex; align-items: center; gap: 28px; }
.cf-brand { display: inline-flex; align-items: center; gap: 8px; color: var(--cf-text); text-decoration: none; }
.cf-brand:hover { text-decoration: none; }
.cf-brand__mark { width: 30px; height: 30px; display: grid; place-items: center; border-radius: 9px; color: var(--cf-neutral-50); background: linear-gradient(140deg, var(--cf-brand-500), var(--cf-accent-600)); box-shadow: var(--cf-shadow-brand); }
.cf-brand__mark svg { width: 17px; height: 17px; }
.cf-brand__word { font-weight: 800; font-size: 17px; letter-spacing: 0.01em; }
.cf-brand__en { font-size: 12px; font-weight: 500; color: var(--cf-text-tertiary); margin-left: 2px; }
.cf-nav { display: flex; gap: 2px; margin-left: 6px; }
.cf-nav__link { padding: 7px 12px; border-radius: 8px; font-size: 14px; color: var(--cf-text-secondary); }
.cf-nav__link:hover { color: var(--cf-text); background: var(--cf-surface-hover); text-decoration: none; }
.cf-nav__link--active { color: var(--cf-text); font-weight: 600; background: color-mix(in srgb, var(--cf-brand-500) 8%, transparent); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--cf-brand-500) 18%, transparent); }
.cf-header__actions { margin-left: auto; display: flex; align-items: center; gap: 10px; }

/* 按钮基元 */
.cf-btn { display: inline-flex; align-items: center; justify-content: center; gap: 7px; height: 36px; padding: 0 15px; border-radius: 9px; font-size: 14px; font-weight: 600; border: 1px solid transparent; cursor: pointer; transition: transform 0.14s ease, box-shadow 0.2s ease, border-color 0.2s ease, background-color 0.2s ease; }
.cf-btn:hover { text-decoration: none; }
.cf-btn--primary { background: linear-gradient(120deg, var(--cf-brand-500), var(--cf-accent-600)); color: var(--cf-neutral-50); box-shadow: var(--cf-shadow-brand); }
.cf-btn--primary:hover { transform: translateY(-1px); box-shadow: 0 8px 22px color-mix(in srgb, var(--cf-brand-500) 46%, transparent); }
.cf-btn__arrow { transition: transform 0.16s ease; }
.cf-btn--primary:hover .cf-btn__arrow { transform: translateX(2px); }
.cf-btn--ghost { color: var(--cf-text); border-color: var(--cf-line-strong); background: var(--cf-surface); }
.cf-btn--ghost:hover { border-color: color-mix(in srgb, var(--cf-brand-500) 55%, var(--cf-line)); color: var(--cf-brand-500); transform: translateY(-1px); }

.cf-main { flex: 1; position: relative; z-index: 1; }

/* ── 页脚 ── */
.cf-footer { position: relative; z-index: 1; margin-top: 72px; border-top: 1px solid var(--cf-line); background: color-mix(in srgb, var(--cf-bg-elev) 60%, transparent); padding: 44px 0 28px; }
.cf-footer__grid { display: grid; grid-template-columns: minmax(0, 1.6fr) repeat(2, minmax(120px, 0.6fr)); gap: 40px; }
.cf-footer__blurb { margin-top: 12px; font-size: 13px; line-height: 1.75; color: var(--cf-text-secondary); max-width: 46ch; }
.cf-footer__title { font-size: 12px; font-weight: 700; letter-spacing: 0.1em; text-transform: uppercase; color: var(--cf-text-tertiary); margin-bottom: 12px; }
.cf-footer__col { display: flex; flex-direction: column; gap: 8px; }
.cf-footer__link { font-size: 13.5px; color: var(--cf-text-secondary); }
.cf-footer__link:hover { color: var(--cf-brand-500); text-decoration: none; }
.cf-footer__bottom { margin-top: 36px; padding-top: 18px; border-top: 1px solid var(--cf-line); display: flex; justify-content: space-between; align-items: center; gap: 10px; font-size: 12.5px; color: var(--cf-text-tertiary); }
.cf-footer__status { display: inline-flex; align-items: center; gap: 8px; color: var(--cf-text-secondary); }

/* 窄屏门户：压缩间距，导航与 CTA 收敛为「图标+短文案」 */
@media (width < 860px) {
  .cf-header__inner { gap: 12px; }
  .cf-nav { margin-left: 0; }
  .cf-nav__link { padding: 6px 8px; font-size: 13px; }
}

@media (width < 720px) {
  .cf-wrap { width: min(1120px, calc(100% - var(--cf-space-4))); }
  .cf-header__inner { height: 56px; gap: 8px; }
  .cf-brand__en { display: none; }
  .cf-nav { gap: 0; }
  .cf-nav__link { padding: 6px 7px; font-size: 12.5px; }
  .cf-nav__link:nth-child(3), .cf-nav__link:nth-child(4) { display: none; }
  .cf-btn { height: 32px; padding: 0 11px; font-size: 13px; }
  .cf-header__actions { gap: 6px; }
  .cf-footer__grid { grid-template-columns: 1fr 1fr; gap: 28px; }
  .cf-footer__brand { grid-column: 1 / -1; }
  .cf-footer__bottom { flex-direction: column; align-items: flex-start; gap: 8px; }
}
</style>
