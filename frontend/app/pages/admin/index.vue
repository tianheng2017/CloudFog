<script setup lang="ts">
definePageMeta({ layout: 'admin' })
const store = useUserStore()
if (!store.loaded) await store.fetchMe()
const links = [
  { to: '/admin/channels', label: '渠道管理', desc: '供应商渠道状态/倍率（凭证仅显示加密标志）' },
  { to: '/admin/users', label: '用户管理', desc: '用户账号、角色与状态' },
  { to: '/admin/models', label: '模型目录', desc: '模型、上下文窗口与计费模式' },
]
</script>

<template>
  <div>
    <h1 class="pg-title">运营概览</h1>
    <p class="pg-sub">{{ store.me ? `${store.me.username}（${store.me.role}）` : '管理后台' }}</p>
    <div class="grid">
      <NuxtLink v-for="l in links" :key="l.to" :to="l.to" class="card">
        <h3>{{ l.label }}</h3>
        <p>{{ l.desc }}</p>
      </NuxtLink>
    </div>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { margin-top: 2px; font-size: 13px; color: var(--cf-text-secondary); }
.grid { margin-top: 22px; display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 14px; }
.card { border: 1px solid var(--cf-line); background: var(--cf-surface); border-radius: 12px; padding: 16px 18px; }
.card:hover { border-color: var(--cf-line-strong); text-decoration: none; }
.card h3 { font-size: 15px; color: var(--cf-text); }
.card p { margin-top: 4px; font-size: 12.5px; color: var(--cf-text-secondary); }
</style>
