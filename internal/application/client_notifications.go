package application

import (
	"context"
	"encoding/json"
	"shopnext-laos/internal/domain"
	"time"
)

type ClientPaymentEvent struct {
	EventID       string    `json:"event_id"`
	Type          string    `json:"type"`
	OrderID       string    `json:"order_id"`
	PaymentStatus string    `json:"payment_status"`
	OrderStatus   string    `json:"order_status"`
	PaidAt        time.Time `json:"paid_at"`
	TotalKip      int64     `json:"total_kip"`
}

func (s *Service) enqueueClientPayment(ctx context.Context, tx Store, order domain.Order, paidAt time.Time) error {
	if s.ClientWebhook == nil {
		return nil
	}
	record := domain.ClientNotification{Base: domain.NewBase(), OrderID: order.BillNumber, Status: "PENDING", AvailableAt: paidAt}
	raw, err := json.Marshal(ClientPaymentEvent{record.ID, "order.payment_succeeded", order.BillNumber, "PAID", "CONFIRMED", paidAt, order.TotalKip})
	if err != nil {
		return err
	}
	record.Payload = raw
	return tx.Insert(ctx, ClientNotifications, &record)
}

// Delivery is at least once: the receiver deduplicates using event_id. Leased
// records survive worker crashes; no external calls run inside transactions.
func (w *Worker) DrainClientNotifications(ctx context.Context) (int, error) {
	s := w.Service
	if s.ClientWebhook == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	var claimed []domain.ClientNotification
	err := s.Store.Transaction(ctx, func(tx Store) error {
		var abandoned []domain.ClientNotification
		if err := tx.Find(ctx, ClientNotifications, Query{Eq: map[string]any{"status": "SENDING"}, LT: map[string]any{"lease_until": now}, Limit: 20, Lock: true, SkipLocked: true}, &abandoned); err != nil {
			return err
		}
		for _, record := range abandoned {
			state := "FAILED"
			if record.Attempts >= 8 {
				state = "DEAD"
			}
			if err := tx.Update(ctx, ClientNotifications, record.ID, changed(map[string]any{"status": state, "lease_until": nil, "available_at": now})); err != nil {
				return err
			}
		}
		if err := tx.Find(ctx, ClientNotifications, Query{In: map[string][]string{"status": {"PENDING", "FAILED"}}, LT: map[string]any{"available_at": now.Add(time.Nanosecond)}, Sort: "available_at", Limit: 20, Lock: true, SkipLocked: true}, &claimed); err != nil {
			return err
		}
		for i, record := range claimed {
			claimed[i].Attempts = record.Attempts + 1
			if err := tx.Update(ctx, ClientNotifications, record.ID, changed(map[string]any{"status": "SENDING", "attempts": claimed[i].Attempts, "lease_until": now.Add(2 * time.Minute)})); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, record := range claimed {
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		sendErr := s.ClientWebhook.Send(ctx, record.ID, record.Payload)
		err := s.Store.Transaction(ctx, func(tx Store) error {
			current, err := findOne[domain.ClientNotification](ctx, tx, ClientNotifications, locked(byID(record.ID)))
			if err != nil {
				return err
			}
			if current.Status != "SENDING" || current.Attempts != record.Attempts {
				return nil
			}
			values := map[string]any{"lease_until": nil}
			if sendErr == nil {
				values["status"] = "SENT"
				values["sent_at"] = time.Now().UTC()
				values["last_error"] = ""
				sent++
			} else {
				state := "FAILED"
				if record.Attempts >= 8 {
					state = "DEAD"
				}
				values["status"] = state
				values["last_error"] = "client webhook did not confirm acceptance"
				values["available_at"] = time.Now().UTC().Add(30 * time.Second * time.Duration(1<<min(record.Attempts-1, 7)))
			}
			return tx.Update(ctx, ClientNotifications, record.ID, changed(values))
		})
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}
