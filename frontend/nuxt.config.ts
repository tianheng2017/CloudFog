// 云之雾前端（Nuxt 4.2）——配置唯一权威见 docs/14-frontend.md §1.2/§5。
// 三层路由策略（§1.2/§11 F16）：公开门户 SSR（SEO）；console/admin CSR（私有，禁公开缓存）。
export default defineNuxtConfig({
  compatibilityDate: '2026-09-07',

  srcDir: 'app/',
  ssr: true,

  devtools: { enabled: false },

  // Element Plus 模块注入（基础组件，观感由 design-tokens.css 深度定制，§1.2）
  modules: ['@element-plus/nuxt', '@pinia/nuxt'],

  // 顺序：Element Plus 官方暗色变量铺底 → tokens 覆盖（--cf-* + --el-* 桥接）→ 全局基元
  css: [
    'element-plus/theme-chalk/dark/css-vars.css',
    '~/assets/css/tokens.css',
    '~/assets/css/main.css',
  ],

  app: {
    head: {
      htmlAttrs: { lang: 'zh-CN' },
      title: '云之雾 CloudFog',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
      script: [
        // 首帧防 FOUC：SSR HTML 到达即读 cf-theme/系统偏好设置 html.dark（在 CSS 应用前生效）
        {
          innerHTML: `(function(){try{var t=localStorage.getItem('cf-theme');var d=t?t==='dark':window.matchMedia('(prefers-color-scheme: dark)').matches;if(d)document.documentElement.classList.add('dark')}catch(e){}})();`,
          tagPriority: 'critical',
        },
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
    // public.apiBase：浏览器同域 /api（生产 ingress 同源）；apiServerBase：SSR 服务端直连后端绝对基址
    //（nuxt dev 走 nitro.devProxy 可留默认；生产由 NUXT_API_SERVER_BASE 注入容器网络地址）
    apiServerBase: 'http://127.0.0.1:8080',
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
