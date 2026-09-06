package model

import (
	"database/sql/driver"
	"fmt"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Decimal 直接承载 shopspring/decimal，经 GormDBDataType + Valuer/Scanner 直通
// numeric 列（02 §15.3/§14：金额全程无 float64）。
// pgx 对 numeric 默认返回 string（或 []byte），Scan 分支与之一一对应。
type Decimal struct{ decimal.Decimal }

func (Decimal) GormDBDataType(*gorm.DB, *schema.Field) string {
	return "numeric(20,10)" // 单价与金额列的统一精度（02 §14.1）
}

func (d Decimal) Value() (driver.Value, error) {
	// 必须返回 driver 基本类型字符串，而非内嵌 decimal.Decimal 结构体：
	// 返回结构体会让 pgx 对零值误判（曾实测零值被编码成 bool true，insert 报错）。
	return d.String(), nil
}

func (d *Decimal) Scan(v any) error {
	switch t := v.(type) {
	case nil:
		d.Decimal = decimal.Zero
	case string:
		dv, err := decimal.NewFromString(t)
		if err != nil {
			return err
		}
		d.Decimal = dv
	case []byte:
		return d.Scan(string(t))
	default:
		return fmt.Errorf("unsupported type for model.Decimal: %T", v)
	}
	return nil
}
