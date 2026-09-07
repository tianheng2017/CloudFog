// 云之雾前端（Nuxt 4.2）——配置唯一权威见 docs/14-frontend.md §1.2/§5。
// 三层路由策略（§1.2/§11 F16）：公开门户 SSR（SEO）；console/admin CSR（私有，禁公开缓存）。
// dev 不缓存公开页（swr 只作用于生产，便于本地迭代即时可见）。
const isDev = process.env.NODE_ENV !== 'production'
export default defineNuxtConfig({
  compatibilityDate: '2026-09-07',

  srcDir: 'app/',
  ssr: true,

  devtools: { enabled: false },

  // Element Plus（观感由 design-tokens.css 深度定制）+ Pinia
  modules: ['@element-plus/nuxt', '@pinia/nuxt'],
  // 注：@nuxtjs/sitemap+@nuxtjs/robots 的 server 运行时与本栈（vite8/nitro 2.13）打包冲突待专项
  // 解决（F16 sitemap.xml 自动化列为后续迭代；robots.txt 先以 public/ 静态提供）。

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
    // 公开门户（SEO 收载；dev 不缓存）
    '/': isDev ? {} : { swr: 60 },
    '/models/**': isDev ? {} : { swr: 60 },
    '/announcements/**': isDev ? {} : { swr: 60 },
    // 私有控制台/认证页：CSR + 无公开缓存（F16）
    '/console/**': { ssr: false },
    '/admin/**': { ssr: false },
    '/login': { ssr: false },
    '/register': { ssr: false },
  },

  runtimeConfig: {
    // public.apiBase：浏览器同域 /api（生产 ingress 同源）；apiServerBase：SSR 服务端直连后端绝对基址
    //（nuxt dev 走 nitro.devProxy 可留默认；生产由 NUXT_API_SERVER_BASE 注入容器网络地址）
    apiServerBase: 'http://127.0.0.1:8080',
    public: {
      apiBase: '/api',
      // canonical/OG 站点基址（部署 NUXT_PUBLIC_SITE_URL 覆盖）
      siteUrl: 'https://api.cloudfog.example',
    },
  },

  nitro: {
    devProxy: {
      '/api': { target: 'http://127.0.0.1:8080/api', changeOrigin: true },
    },
  },

  typescript: { strict: true },
})
