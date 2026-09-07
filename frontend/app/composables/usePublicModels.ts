// 公开模型目录（门户 SSR 数据源）。后端 public 端点（B4-5）未就绪时优雅空态。
// 字段与 b45 /api/v1/public/models 对齐：name/display_name/provider_code/context_window/基础价可选。
export interface PublicModel {
  id?: number
  name: string
  display_name?: string
  provider_code?: string
  context_window?: number
  input_price_per_1k?: string
  output_price_per_1k?: string
}

export function usePublicModels() {
  return useAsyncData<PublicModel[]>('public-models', () =>
    $fetch<PublicModel[]>(apiUrl('/v1/public/models')).catch(() => []),
    { default: () => [] },
  )
}
