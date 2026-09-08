// 安全响应头（2026-09-08 补齐）：此前门户/控制台/管理端零安全头。
// 说明：
// - CSP 允许 'unsafe-inline'：Nuxt SSR 会内联首帧主题脚本与 payload、Element Plus 运行时会注入样式。
//   收紧方案（nonce/hash 化）见 docs/14 §11 后续迭代；object-src/base-uri/frame-ancestors 已收紧。
// - HSTS 不在此下发：TLS 由前置 Nginx/Caddy 终结，避免 HTTP 直连场景被浏览器强制跳 HTTPS。
export default defineNitroPlugin((nitroApp) => {
  nitroApp.hooks.hook('render:response', (_response, { event }) => {
    setHeaders(event, {
      'X-Content-Type-Options': 'nosniff',
      'Referrer-Policy': 'strict-origin-when-cross-origin',
      'X-Frame-Options': 'DENY',
      'Permissions-Policy': 'geolocation=(), microphone=(), camera=()',
      'Content-Security-Policy': [
        "default-src 'self'",
        "script-src 'self' 'unsafe-inline'",
        "style-src 'self' 'unsafe-inline'",
        "img-src 'self' data:",
        "connect-src 'self'",
        "font-src 'self'",
        "object-src 'none'",
        "base-uri 'self'",
        "frame-ancestors 'none'",
      ].join('; '),
    })
  })
})
