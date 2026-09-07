// 时间/金额展示格式化（14-frontend §1.3 utils）。后端一律返回 ISO/RFC3339 时间戳，
// 页面展示必须人类可读（年月日时分秒 或 年月日），禁止直出 ISO 字符串。
const pad = (n: number) => String(n).padStart(2, '0')

function parse(v?: string | number | null): Date | null {
  if (v === undefined || v === null || v === '') return null
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? null : d
}

/** 本地时区 2026-09-07 21:09:08 */
export function formatDateTime(v?: string | number | null): string {
  const d = parse(v)
  if (!d) return '—'
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** 本地时区 2026-09-07 */
export function formatDate(v?: string | number | null): string {
  const d = parse(v)
  if (!d) return '—'
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
