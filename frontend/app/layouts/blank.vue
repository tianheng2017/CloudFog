<template>
  <div class="b-wrap">
    <slot />
  </div>
</template>

<style scoped>
.b-wrap { min-height: 100vh; display: flex; align-items: center; justify-content: center; background: radial-gradient(1200px 600px at 70% -10%, color-mix(in srgb, var(--cf-brand-500) 18%, transparent), transparent), var(--cf-bg); }
</style>
