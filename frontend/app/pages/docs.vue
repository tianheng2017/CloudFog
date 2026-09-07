<script setup lang="ts">
// 开发者文档（公开 SSR）。MVP：快速接入指南（静态内容，SEO 收载）。
definePageMeta({ layout: 'default' })
usePageSeo({
  title: '开发者文档 — 快速接入 — 云之雾',
  description: '3 分钟接入云之雾：获取 API Key、配置 base_url、完成首次对话。OpenAI SDK 兼容。',
})
const steps = [
  { t: '注册并创建 API Key', d: '登录控制台 → API 密钥 → 创建密钥。明文仅在创建时展示一次，请妥善保存。' },
  { t: '配置 base_url', d: '将 SDK 的 base_url 指向云之雾网关，Path 保持 /v1 不变（OpenAI 兼容）。' },
  { t: '发起首次对话', d: '使用 Authorization: Bearer sk-cf-… 调用 /v1/chat/completions。用量与费用实时可查。' },
]
</script>

<template>
  <div class="wrap">
    <header class="pg-head"><h1>快速接入</h1><p class="pg-sub">OpenAI 官方 SDK 改 base_url 即可使用</p></header>

    <ol class="steps">
      <li v-for="(s, i) in steps" :key="s.t" class="step">
        <div class="step__no num">{{ i + 1 }}</div>
        <div class="step__body">
          <h2>{{ s.t }}</h2>
          <p>{{ s.d }}</p>
        </div>
      </li>
    </ol>

    <section class="sample">
      <h2>示例请求</h2>
      <pre><code>curl https://api.cloudfog.example/v1/chat/completions \\
  -H "Authorization: Bearer sk-cf-…" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'</code></pre>
    </section>
  </div>
</template>

<style scoped>
.wrap { width: min(820px, 100% - 48px); margin-inline: auto; padding: 40px 0; }
.pg-head h1 { font-size: 30px; font-weight: 800; }
.pg-sub { margin-top: 6px; color: var(--cf-text-secondary); }
.steps { list-style: none; margin-top: 28px; padding: 0; display: flex; flex-direction: column; gap: 14px; counter-reset: step; }
.step { display: flex; gap: 16px; border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: 12px; padding: 16px 20px; }
.step__no { flex: none; width: 28px; height: 28px; border-radius: var(--cf-radius-full); background: linear-gradient(120deg, var(--cf-brand-500), var(--cf-accent-600)); color: var(--cf-text-invert); display: grid; place-items: center; font-size: 14px; font-weight: 700; }
.step__body h2 { font-size: 16px; }
.step__body p { margin-top: 4px; color: var(--cf-text-secondary); }
.sample { margin-top: 28px; }
.sample h2 { font-size: 18px; margin-bottom: 10px; }
pre { background: var(--cf-surface); border: 1px solid var(--cf-line); border-radius: 12px; padding: 16px 20px; overflow-x: auto; font-size: 13px; line-height: 1.7; color: var(--cf-text); }
</style>
