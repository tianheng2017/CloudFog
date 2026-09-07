package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// ── 支付订单（02 §7.3 / 06 §7）────────────────────────────────

// CreatePaymentOrder 落库订单（order_no 唯一；撞唯一返回原始错误，调用方重生成重试）。
func (r *Repository) CreatePaymentOrder(ctx context.Context, o *model.PaymentOrder) error {
	return r.db.WithContext(ctx).Create(o).Error
}

// PaymentOrderByNo 按平台订单号查询；未命中返回 (nil, nil)。
func (r *Repository) PaymentOrderByNo(ctx context.Context, orderNo string) (*model.PaymentOrder, error) {
	var o model.PaymentOrder
	err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// PaymentOrderByTradeNo 按渠道流水号查询（webhook 幂等定位，provider_trade_no 部分唯一）。
func (r *Repository) PaymentOrderByTradeNo(ctx context.Context, tradeNo string) (*model.PaymentOrder, error) {
	var o model.PaymentOrder
	err := r.db.WithContext(ctx).Where("provider_trade_no = ?", tradeNo).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
