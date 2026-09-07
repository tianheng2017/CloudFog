// 公开页 SEO 封装（F16）：逐页 title/description + canonical + OG（og:type/og:url/og:locale）。
// canonical 以站点基址 + 当前 path 生成（部署经 NUXT_PUBLIC_SITE_URL 覆盖）。
export function usePageSeo(opts: { title: string; description: string }) {
  const route = useRoute()
  const cfg = useRuntimeConfig()
  const siteUrl = (cfg.public.siteUrl as string) || 'https://api.cloudfog.example'
  const url = siteUrl.replace(/\/$/, '') + route.path
  useSeoMeta({
    title: opts.title,
    description: opts.description,
    ogTitle: opts.title,
    ogDescription: opts.description,
    ogType: 'website',
    ogUrl: url,
    ogLocale: 'zh_CN',
  })
  useHead({ link: [{ rel: 'canonical', href: url }] })
}
