<script setup lang="ts">
// 用户控制台概览：余额/今日用量入口卡 + 第一步引导链（F1/F14）
definePageMeta({ layout: 'console' })
const store = useUserStore()
if (!store.loaded) await store.fetchMe()

const cards = [
  { to: '/console/keys', icon: '🔑', title: 'API 密钥', desc: '创建与管理密钥（明文仅创建时可见一次）' },
  { to: '/console/usage', icon: '📊', title: '用量', desc: '今日/本月统计与逐条调用明细' },
  { to: '/console/billing', icon: '💳', title: '账单与余额', desc: '余额、充值入账与结算流水' },
]

const { data: bal } = useApiResource<{ balance: string } | null>(
  () => apiFetch('/api/v1/me/balance'), null)
</script>

<template>
  <div>
    <h1 class="pg-title">概览</h1>
    <p class="pg-sub">
      欢迎回来{{ store.me ? `，${store.me.username}` : '' }}<span v-if="bal"> · 可用余额 <span class="num" style="font-weight: 650">$ {{ bal.balance }}</span></span>
    </p>

    <div class="grid">
      <NuxtLink v-for="c in cards" :key="c.to" :to="c.to" class="card">
        <div class="card__icon">{{ c.icon }}</div>
        <h3>{{ c.title }}</h3>
        <p>{{ c.desc }}</p>
      </NuxtLink>
    </div>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { margin-top: 4px; color: var(--cf-text-secondary); }
.grid { margin-top: 24px; display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 16px; }
.card { border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: 14px; padding: 20px; transition: border-color 0.15s, transform 0.15s; }
.card:hover { border-color: var(--cf-line-strong); transform: translateY(-2px); text-decoration: none; }
.card__icon { font-size: 22px; }
.card h3 { margin-top: 10px; font-size: 16px; color: var(--cf-text); }
.card p { margin-top: 6px; font-size: 13px; color: var(--cf-text-secondary); }
</style>
