// API 路径解析（docs/14-frontend.md §1.2）：客户端同域 /api（ingress 同源 + HttpOnly Cookie）；
// SSR 服务端必须用绝对基址直连后端（devProxy 仅 nuxt dev 生效，生产 preview/.output 无代理）。
export function apiUrl(path: string) {
  const cfg = useRuntimeConfig()
  const base = import.meta.server ? cfg.apiServerBase : ''
  return `${base}${cfg.public.apiBase}${path.startsWith('/') ? path : `/${path}`}`
}
