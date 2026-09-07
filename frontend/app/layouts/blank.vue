<script setup lang="ts">
// 认证/入口布局（login/register）：EP 组件需 zh-cn locale
import zhCn from 'element-plus/es/locale/lang/zh-cn'
</script>

<template>
  <ElConfigProvider :locale="zhCn">
    <div class="b-wrap">
      <slot />
    </div>
  </ElConfigProvider>
</template>

<style scoped>
.b-wrap { min-height: 100vh; display: flex; align-items: center; justify-content: center; background: radial-gradient(1200px 600px at 70% -10%, color-mix(in srgb, var(--cf-brand-500) 18%, transparent), transparent), var(--cf-bg); }
</style>
