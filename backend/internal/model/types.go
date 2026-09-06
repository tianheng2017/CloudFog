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
	return d.Decimal, nil
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
