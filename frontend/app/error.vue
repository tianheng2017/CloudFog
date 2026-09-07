<script setup lang="ts">
// 全局错误页（F14 错误态）：按状态码呈现 404/500 与品牌观感；不泄露内部错误细节
import type { NuxtError } from '#app'

const props = defineProps<{ error: NuxtError }>()
useHead({ title: computed(() => (props.error.statusCode === 404 ? '页面不存在 — 云之雾' : '出错了 — 云之雾')) })

const is404 = computed(() => props.error.statusCode === 404)
const heading = computed(() => (is404.value ? '页面不存在' : '服务开小差了'))
const desc = computed(() => (is404.value ? '你访问的链接可能已失效或地址有误。' : '请稍后重试；如果问题持续，请联系我们。'))

async function backHome() {
  await clearError({ redirect: '/' })
}
async function toConsole() {
  await clearError({ redirect: '/console' })
}
</script>

<template>
  <main class="err">
    <div class="err__code num">{{ error.statusCode || 500 }}</div>
    <h1 class="err__title">{{ heading }}</h1>
    <p class="err__desc">{{ desc }}</p>
    <div class="err__actions">
      <button type="button" class="btn btn--primary" @click="backHome">返回首页</button>
      <button type="button" class="btn btn--ghost" @click="toConsole">进入控制台</button>
    </div>
  </main>
</template>

<style scoped>
.err { min-height: 100vh; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 24px; text-align: center; background: radial-gradient(900px 420px at 50% -10%, color-mix(in srgb, var(--cf-brand-500) 14%, transparent), transparent), var(--cf-bg); }
.err__code { font-size: clamp(64px, 12vw, 120px); font-weight: 800; line-height: 1; letter-spacing: -0.03em; background: linear-gradient(120deg, var(--cf-brand-400), var(--cf-accent-500)); -webkit-background-clip: text; background-clip: text; color: transparent; }
.err__title { font-size: 24px; font-weight: 700; }
.err__desc { color: var(--cf-text-secondary); }
.err__actions { margin-top: 16px; display: flex; gap: 12px; }
.btn { display: inline-flex; align-items: center; height: 42px; padding: 0 20px; border-radius: 10px; font-weight: 600; border: 1px solid transparent; cursor: pointer; font-size: 14px; }
.btn--primary { background: linear-gradient(120deg, var(--cf-brand-500), var(--cf-accent-600)); color: var(--cf-text-invert); }
.btn--ghost { background: var(--cf-surface); color: var(--cf-text); border-color: var(--cf-line-strong); }
</style>
