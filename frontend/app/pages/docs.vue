<script setup lang="ts">
// 开发者文档（公开 SSR）。MVP：快速接入指南（静态内容，SEO 收载）。v2 文档式排版
definePageMeta({ layout: 'default' })
usePageSeo({
  title: '开发者文档 — 快速接入 — 云之雾',
  description: '3 分钟接入云之雾：获取 API Key、配置 base_url、完成首次对话。OpenAI SDK 兼容。',
})
const steps = [
  { id: 'step-1', t: '注册并创建 API Key', d: '登录控制台 → API 密钥 → 创建密钥。明文仅在创建时展示一次，请妥善保存，服务端不保存明文。' },
  { id: 'step-2', t: '配置 base_url', d: '将 SDK 的 base_url 指向云之雾网关，Path 保持 /v1 不变（OpenAI 兼容）。' },
  { id: 'step-3', t: '发起首次对话', d: '使用 Authorization: Bearer sk-cf-… 调用 /v1/chat/completions。用量与费用实时可查。' },
]
const toc = [
  { href: '#quickstart', label: '快速接入' },
  ...steps.map((s, i) => ({ href: `#${s.id}`, label: `${i + 1}. ${s.t}` })),
  { href: '#example', label: '示例请求' },
]
const curl = `curl https://api.cloudfog.example/v1/chat/completions \\
  -H "Authorization: Bearer sk-cf-…" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'`
const copied = ref(false)
async function copyCurl() {
  if (!import.meta.client) return
  try {
    await navigator.clipboard.writeText(curl.replaceAll(' \\\n', ' '))
    copied.value = true
    setTimeout(() => { copied.value = false }, 1800)
  } catch { /* 剪贴板不可用忽略 */ }
}
</script>

<template>
  <div class="cf-page cf-page--narrow doc">
    <!-- 顶部文档元信息 -->
    <header class="doc-head">
      <span class="cf-eyebrow">Developer Docs</span>
      <h1 class="cf-h1">快速接入</h1>
      <p class="cf-sub">OpenAI 官方 SDK，只改一行 base_url 即可使用云之雾网关。</p>
      <div class="doc-head__tags">
        <span class="cf-tag cf-tag--success">OpenAI 兼容</span>
        <span class="cf-tag cf-tag--muted">~ 3 分钟</span>
        <span class="cf-tag cf-tag--info">v1</span>
      </div>
    </header>

    <div class="doc-layout">
      <!-- 侧栏 TOC（桌面 sticky） -->
      <aside class="doc-toc" aria-label="本页目录">
        <div class="doc-toc__title">本页目录</div>
        <a v-for="t in toc" :key="t.href" :href="t.href" class="doc-toc__link">{{ t.label }}</a>
        <div class="doc-toc__foot">
          <NuxtLink to="/console" class="cf-go">去控制台创建密钥<svg viewBox="0 0 16 16" width="13" height="13"><path d="M2 8h11M9 3l5 5-5 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg></NuxtLink>
        </div>
      </aside>

      <article class="doc-body">
        <section id="quickstart" class="doc-sec">
          <div v-for="(s, i) in steps" :key="s.id" :id="s.id" class="doc-step">
            <div class="doc-step__no num">{{ i + 1 }}</div>
            <div>
              <h2>{{ s.t }}</h2>
              <p>{{ s.d }}</p>
            </div>
          </div>
          <div class="note">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="M12 3l7 3v5c0 5-3.5 8-7 10-3.5-2-7-5-7-10V6l7-3z" stroke-linejoin="round" /><path d="M12 8v4m0 4h.01" stroke-linecap="round" /></svg>
            <p><b>安全提醒：</b>API Key 明文只在创建时展示一次。不要将密钥提交到 Git 或写进前端代码，泄露后可随时在控制台吊销。</p>
          </div>
        </section>

        <section id="example" class="doc-sec">
          <h2 class="doc-sec__title">示例请求</h2>
          <div class="code">
            <div class="code__bar">
              <span class="code__path num">terminal</span>
              <button type="button" class="code__copy" @click="copyCurl">{{ copied ? '已复制 ✓' : '复制' }}</button>
            </div>
            <pre><code><span class="t-c"># 一次调用，一条命令</span>
<span class="t-p">curl</span> https://api.cloudfog.example/v1/chat/completions \
  -H <span class="t-s">"Authorization: Bearer sk-cf-…"</span> \
  -H <span class="t-s">"Content-Type: application/json"</span> \
  -d <span class="t-s">'{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'</span></code></pre>
          </div>
          <p class="doc-hint">期望响应：<code>HTTP/1.1 200 OK</code>，返回体与 OpenAI 官方格式完全一致；调用后可在控制台「用量 / 账单」查看该笔明细。</p>
        </section>
      </article>
    </div>
  </div>
</template>

<style scoped>
.doc { padding-top: clamp(44px, 7vw, 72px); }
.doc-head { padding-bottom: 26px; border-bottom: 1px solid var(--cf-line); margin-bottom: 30px; }
.doc-head__tags { margin-top: 16px; display: flex; gap: 8px; }

.doc-layout { display: grid; grid-template-columns: 190px minmax(0, 1fr); gap: 44px; align-items: start; }
.doc-toc { position: sticky; top: 88px; display: flex; flex-direction: column; gap: 3px; }
.doc-toc__title { font-size: 11px; font-weight: 700; letter-spacing: 0.1em; text-transform: uppercase; color: var(--cf-text-tertiary); margin-bottom: 8px; }
.doc-toc__link { padding: 5px 10px; border-radius: 6px; font-size: 13px; color: var(--cf-text-secondary); border-left: 2px solid transparent; }
.doc-toc__link:hover { color: var(--cf-text); background: var(--cf-surface-hover); text-decoration: none; }
.doc-toc__foot { margin-top: 14px; padding: 12px 10px 0; border-top: 1px solid var(--cf-line); }

.doc-body { min-width: 0; display: flex; flex-direction: column; gap: 34px; }
.doc-step { display: flex; gap: 18px; padding: 18px 0 8px; border-bottom: 1px solid var(--cf-line); scroll-margin-top: 90px; }
.doc-step__no { flex: none; width: 30px; height: 30px; border-radius: var(--cf-radius-full); display: grid; place-items: center; font-size: 13.5px; font-weight: 700; color: var(--cf-neutral-50); background: linear-gradient(140deg, var(--cf-brand-500), var(--cf-accent-600)); box-shadow: var(--cf-shadow-sm); }
.doc-step h2 { font-size: 17px; font-weight: 700; letter-spacing: -0.01em; scroll-margin-top: 90px; }
.doc-step p { margin-top: 6px; font-size: 14px; line-height: 1.75; color: var(--cf-text-secondary); }

.note { display: flex; gap: 12px; margin-top: 22px; padding: 14px 16px; border: 1px solid color-mix(in srgb, var(--cf-semantic-warning) 40%, var(--cf-line)); border-radius: var(--cf-radius-lg); background: color-mix(in srgb, var(--cf-semantic-warning) 7%, var(--cf-surface)); color: var(--cf-text-secondary); }
.note svg { flex: none; margin-top: 2px; color: var(--cf-semantic-warning); }
.note p { font-size: 13.5px; line-height: 1.7; }
.note b { color: var(--cf-text); }

.doc-sec__title { font-size: 20px; font-weight: 800; letter-spacing: -0.018em; scroll-margin-top: 90px; margin-bottom: 14px; }

/* 深色代码窗（恒定深底，明暗双态一致） */
.code { border-radius: var(--cf-radius-lg); border: 1px solid var(--cf-neutral-800); background: var(--cf-neutral-950); overflow: hidden; box-shadow: var(--cf-shadow-md); }
.code__bar { display: flex; align-items: center; justify-content: space-between; padding: 8px 14px; border-bottom: 1px solid var(--cf-neutral-800); }
.code__path { font-size: 11.5px; color: var(--cf-neutral-600); text-align: left; }
.code__copy { font: inherit; font-size: 12px; color: var(--cf-neutral-300); background: transparent; border: 1px solid var(--cf-neutral-700); border-radius: 6px; padding: 2px 10px; cursor: pointer; transition: color 0.15s ease, border-color 0.15s ease; }
.code__copy:hover { color: var(--cf-neutral-50); border-color: var(--cf-brand-500); }
.code pre { margin: 0; padding: 18px 20px; overflow-x: auto; }
.code code { font-size: 12.8px; line-height: 1.85; color: var(--cf-neutral-100); }
.t-c { color: var(--cf-neutral-600); }
.t-s { color: var(--cf-semantic-success); }
.t-p { color: var(--cf-semantic-info); }
.doc-hint { margin-top: 12px; font-size: 13px; color: var(--cf-text-tertiary); }
.doc-hint code { font-size: 12px; color: var(--cf-brand-500); }

@media (width < 900px) {
  .doc-layout { grid-template-columns: 1fr; }
  .doc-toc { position: static; flex-direction: row; flex-wrap: wrap; gap: 6px; }
  .doc-toc__title, .doc-toc__foot { display: none; }
  .doc-toc__link { border-left: 0; background: var(--cf-surface); border: 1px solid var(--cf-line); }
}
</style>
