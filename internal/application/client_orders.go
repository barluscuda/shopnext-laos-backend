package application

import (
	"context"
	"fmt"
	"shopnext-laos/internal/domain"
	"strings"
	"time"
	"unicode/utf8"
)

type BillSummary struct {
	OrderID              string     `json:"order_id"`
	BillNumber           string     `json:"bill_number"`
	PaymentStatus        string     `json:"payment_status"`
	OrderStatus          string     `json:"order_status"`
	PaymentSuccessful    bool       `json:"payment_successful"`
	PickupCode           string     `json:"pickup_code"`
	PickupCodeReady      bool       `json:"pickup_code_ready"`
	CreatedAt            time.Time  `json:"created_at"`
	TotalKip             int64      `json:"total_kip"`
	ReservationExpiresAt *time.Time `json:"reservation_expires_at"`
}

func (s *Service) BillSummary(ctx context.Context, bill string) (BillSummary, error) {
	var result BillSummary
	if _, err := s.Expire(ctx, bill); err != nil {
		return result, err
	}
	order, err := findOne[domain.Order](ctx, s.Store, Orders, Query{Eq: map[string]any{"bill_number": bill}})
	if err != nil {
		return result, err
	}
	return BillSummary{order.BillNumber, order.BillNumber, order.PaymentStatus, order.OrderStatus, order.PaymentStatus == "PAID", order.PickupCode, order.PickupCode != "", order.CreatedAt, order.TotalKip, order.ReservationExpiresAt}, nil
}

func (s *Service) SetPickupCode(ctx context.Context, actor domain.Actor, bill, code, ip string) error {
	if err := Require(actor, "orders.act"); err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if utf8.RuneCountInString(code) < 1 || utf8.RuneCountInString(code) > 80 {
		return domain.Fail("INVALID_PICKUP_CODE", 400)
	}
	for _, r := range code {
		if r < 32 || r == 127 {
			return domain.Fail("INVALID_PICKUP_CODE", 400)
		}
	}
	return s.Store.Transaction(ctx, func(tx Store) error {
		order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": bill}, Lock: true})
		if err != nil {
			return err
		}
		if order.OrderStatus != "CONFIRMED" {
			return domain.Fail("INVALID_STATE", 409)
		}
		if order.PickupCode == code {
			return nil
		}
		if err := tx.Update(ctx, Orders, bill, map[string]any{"pickup_code": code}); err != nil {
			return err
		}
		if err := event(ctx, tx, bill, "PICKUP_CODE_ASSIGNED", "staff assigned pickup code"); err != nil {
			return err
		}
		if err := s.Audit(ctx, tx, actor, "order.pickup_code", "order", bill, "staff assigned pickup code", ip); err != nil {
			return err
		}
		return enqueue(ctx, tx, "sms:pickup:"+bill+":"+domain.Hash(code), order.RecipientPhone, fmt.Sprintf("ShopNext Laos: ບິນ %s ລະຫັດຮັບ %s", bill, code))
	})
}
