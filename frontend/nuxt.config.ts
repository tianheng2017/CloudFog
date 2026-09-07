// 云之雾前端（Nuxt 4.2）——配置唯一权威见 docs/14-frontend.md §1.2/§5。
// 三层路由策略（§1.2/§11 F16）：公开门户 SSR（SEO）；console/admin CSR（私有，禁公开缓存）。
export default defineNuxtConfig({
  compatibilityDate: '2026-09-07',

  srcDir: 'app/',
  ssr: true,

  devtools: { enabled: false },

  // Element Plus 模块注入（基础组件，观感由 design-tokens.css 深度定制，§1.2）
  modules: ['@element-plus/nuxt', '@pinia/nuxt'],

  css: ['~/assets/css/tokens.css', '~/assets/css/main.css'],

  app: {
    head: {
      htmlAttrs: { lang: 'zh-CN' },
      title: '云之雾 CloudFog',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
    },
  },

  routeRules: {
    // 公开门户（SEO 收载）
    '/': { swr: 60 },
    '/models/**': { swr: 60 },
    '/announcements/**': { swr: 60 },
    // 私有控制台：CSR + 无公开缓存（F16）
    '/console/**': { ssr: false },
    '/admin/**': { ssr: false },
    '/login': { ssr: false },
  },

  runtimeConfig: {
    // 后端基址：dev 同源代理（见 nitro.devProxy）；生产同域 ingress 由部署编排保证
    public: {
      apiBase: '/api',
    },
  },

  nitro: {
    devProxy: {
      '/api': { target: 'http://127.0.0.1:8080/api', changeOrigin: true },
    },
  },

  typescript: { strict: true },
})
