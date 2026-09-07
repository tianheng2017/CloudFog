<script setup lang="ts">
import zhCn from 'element-plus/es/locale/lang/zh-cn'
// 主题切换（暗色一等公民 §8.2）：默认跟随系统；显式选择存 cf-theme，html.dark 驱动 tokens
const dark = ref(false)
const sync = () => document.documentElement.classList.toggle('dark', dark.value)
if (import.meta.client) {
  onMounted(() => {
    const saved = localStorage.getItem('cf-theme')
    dark.value = saved === 'dark' || (saved !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches)
    sync()
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
      if (!localStorage.getItem('cf-theme')) { dark.value = e.matches; sync() }
    })
  })
  watch(dark, (d) => { localStorage.setItem('cf-theme', d ? 'dark' : 'light'); sync() })
}
// 提供给子组件切换（控制台顶栏主题开关）
provide('theme-toggle', (d: boolean) => { dark.value = d })
</script>

<template>
  <ElConfigProvider :locale="zhCn" namespace="el">
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
  </ElConfigProvider>
</template>
