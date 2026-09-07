// 滚动入场指令 v-reveal。两端注册（SSR 只需可解析，SSR 不调用 mounted）；
// 客户端用 IntersectionObserver 加 .is-in。隐藏态仅在 html.js 下生效（SEO 安全，见 main.css）。
// 用法：<div class="cf-reveal" v-reveal :style="{ '--d': `${i * 80}ms` }">
export default defineNuxtPlugin((nuxtApp) => {
  const client = import.meta.client
  if (client) document.documentElement.classList.add('js')

  let observer: IntersectionObserver | null = null
  if (client && 'IntersectionObserver' in window) {
    observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue
          const el = e.target as HTMLElement
          el.classList.add('is-in')
          observer?.unobserve(el)
        }
      },
      { threshold: 0.12, rootMargin: '0px 0px -8% 0px' },
    )
  }

  nuxtApp.vueApp.directive('reveal', {
    mounted(el: HTMLElement) {
      if (!observer) { el.classList.add('is-in'); return }
      observer.observe(el)
    },
    unmounted(el: HTMLElement) {
      observer?.unobserve(el)
    },
  })
})
