// Package period 账期口径工具（用户可见统计统一北京时间；内部日聚合/对账沿用 UTC）。
// 北京时间 = UTC+8 固定偏移（无夏令时），用 FixedZone 免 tzdata 依赖。
package period

import "time"

// CST 北京时间固定区。
var CST = time.FixedZone("Asia/Shanghai", 8*3600)

// DayBoundsUTC 参数所在北京日历日 [00:00, 24:00) 的 UTC 表示。
// 例如北京 2026-09-07 日界 → from=2026-09-06T16:00:00Z。
func DayBoundsUTC(t time.Time) (from, to time.Time) {
	y, m, d := t.In(CST).Date()
	from = time.Date(y, m, d, 0, 0, 0, 0, CST).UTC()
	to = from.Add(24 * time.Hour)
	return from, to
}

// MonthStartUTC 参数所在北京自然月首日 00:00 的 UTC 表示。
func MonthStartUTC(t time.Time) time.Time {
	y, m, _ := t.In(CST).Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, CST).UTC()
}

// DayLabel 参数所在北京日历日的 YYYY-MM-DD（日志/排期标签）。
func DayLabel(t time.Time) string {
	return t.In(CST).Format("2006-01-02")
}

// DateValue 参数所在北京日历日的 date 列表示（UTC 壁钟取同日日期，供 date 列写入/比对，
// 避免把北京 00:00(=前日 16:00Z) 直接写 date 列产生跨日偏移）。
func DateValue(t time.Time) time.Time {
	y, m, d := t.In(CST).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
