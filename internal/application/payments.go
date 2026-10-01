package application

import (
	"context"
	"errors"
	"fmt"
	"shopnext-laos/internal/domain"
	"time"
)

func (s *Service) CreateAttempt(ctx context.Context, bill, bank, verified string) (domain.Attempt, error) {
	var attempt domain.Attempt
	if _, ok := domain.Banks[bank]; !ok {
		return attempt, domain.Fail("UNSUPPORTED_BANK", 400)
	}
	if _, err := s.Expire(ctx, bill); err != nil {
		return attempt, err
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": bill}, Lock: true})
		if err != nil {
			return err
		}
		if verified == "" || order.RecipientPhone != verified {
			return domain.Fail("FORBIDDEN", 403)
		}
		if order.OrderStatus != "PENDING_PAYMENT" || order.PaymentType != "ONLINE" || order.ReservationExpiresAt == nil || !order.ReservationExpiresAt.After(time.Now()) {
			return domain.Fail("INVALID_STATE", 409)
		}
		var prior []domain.Attempt
		if err := tx.Find(ctx, Attempts, Query{Eq: map[string]any{"order_id": bill}, In: map[string][]string{"status": {"PENDING", "CREATED"}}}, &prior); err != nil {
			return err
		}
		for _, a := range prior {
			if a.Status == "PENDING" && a.CreatedAt.Add(15*time.Second).After(time.Now()) {
				return domain.Fail("PAYMENT_ATTEMPT_IN_PROGRESS", 409)
			}
			if err := tx.Update(ctx, Attempts, a.ID, changed(map[string]any{"status": "FAILED", "last_error": "superseded"})); err != nil {
				return err
			}
		}
		attempt = domain.Attempt{Base: domain.NewBase(), OrderID: bill, Provider: s.Payments.Name(), Bank: bank, AmountKip: order.TotalKip, Status: "PENDING", ExpiresAt: *order.ReservationExpiresAt}
		return tx.Insert(ctx, Attempts, &attempt)
	})
	if err != nil {
		return attempt, err
	}
	qr, providerErr := s.Payments.CreateQR(ctx, bill, bank, attempt.AmountKip)
	err = s.Store.Transaction(ctx, func(tx Store) error {
		order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": bill}, Lock: true})
		if err != nil {
			return err
		}
		current, err := findOne[domain.Attempt](ctx, tx, Attempts, locked(byID(attempt.ID)))
		if err != nil {
			return err
		}
		if current.Status != "PENDING" {
			attempt = current
			return nil
		}
		if providerErr != nil {
			attempt.LastError = "QR generation failed; retry allowed"
			return tx.Update(ctx, Attempts, current.ID, changed(map[string]any{"status": "FAILED", "last_error": attempt.LastError}))
		}
		attempt.QRCode = qr.Code
		attempt.Deeplink = qr.Deeplink
		attempt.ProviderTransactionID = &qr.TransactionID
		attempt.Status = "CREATED"
		if order.OrderStatus != "PENDING_PAYMENT" || !attempt.ExpiresAt.After(time.Now()) {
			attempt.Status = "EXPIRED"
		}
		return tx.Update(ctx, Attempts, current.ID, changed(map[string]any{"provider_transaction_id": qr.TransactionID, "qr_code": qr.Code, "deeplink": qr.Deeplink, "status": attempt.Status}))
	})
	if err != nil {
		return attempt, err
	}
	if providerErr != nil {
		return attempt, domain.Fail("PAYMENT_PROVIDER_UNAVAILABLE", 502)
	}
	return attempt, nil
}
func (s *Service) Webhook(ctx context.Context, raw []byte) (string, error) {
	callback, err := s.Payments.ParseCallback(raw)
	if err != nil {
		return "", err
	}
	outcome := ""
	err = s.Store.Transaction(ctx, func(tx Store) error {
		if err := tx.LockKey(ctx, "webhook:"+callback.DedupeKey); err != nil {
			return err
		}
		record, err := findOne[domain.PaymentEvent](ctx, tx, PaymentEvents, Query{Eq: map[string]any{"dedupe_key": callback.DedupeKey}})
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil && record.ProcessedAt != nil {
			outcome = "DEDUPED"
			return nil
		}
		if errors.Is(err, domain.ErrNotFound) {
			record = domain.PaymentEvent{Base: domain.NewBase(), Provider: s.Payments.Name(), ProviderTransactionID: callback.TransactionID, DedupeKey: callback.DedupeKey, BillNumber: callback.BillNumber, NormalizedStatus: callback.Status, AmountKip: callback.Amount, RawBody: string(raw), Outcome: "PENDING"}
			if err := tx.Insert(ctx, PaymentEvents, &record); err != nil {
				return err
			}
		}
		outcome, err = s.processPaymentEvent(ctx, tx, record)
		return err
	})
	return outcome, err
}
func (s *Service) processPaymentEvent(ctx context.Context, tx Store, record domain.PaymentEvent) (string, error) {
	outcome := "IGNORED"
	now := time.Now().UTC()
	if record.NormalizedStatus == "COMPLETED" {
		attempt, err := findOne[domain.Attempt](ctx, tx, Attempts, Query{Eq: map[string]any{"provider": record.Provider, "provider_transaction_id": record.ProviderTransactionID}})
		if errors.Is(err, domain.ErrNotFound) {
			return "UNMATCHED", tx.Update(ctx, PaymentEvents, record.ID, changed(map[string]any{"outcome": "UNMATCHED"}))
		}
		if err != nil {
			return "", err
		}
		order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": attempt.OrderID}, Lock: true})
		if err != nil {
			return "", err
		}
		attempt, err = findOne[domain.Attempt](ctx, tx, Attempts, locked(byID(attempt.ID)))
		if err != nil {
			return "", err
		}
		switch {
		case record.AmountKip == nil || *record.AmountKip != attempt.AmountKip:
			outcome = "AMOUNT_MISMATCH"
		case record.BillNumber != "" && record.BillNumber != order.BillNumber:
			outcome = "REFERENCE_MISMATCH"
		case attempt.Status == "PAID" && order.PaymentStatus == "PAID":
			outcome = "ALREADY_PAID"
		case !attempt.ExpiresAt.After(now) || order.OrderStatus != "PENDING_PAYMENT" || order.ReservationExpiresAt == nil || !order.ReservationExpiresAt.After(now) || attempt.Status != "CREATED":
			outcome = "RECONCILIATION_REQUIRED"
		default:
			if err := s.transition(ctx, tx, order, "PAID", record.Provider+" "+record.ProviderTransactionID); err != nil {
				return "", err
			}
			if err := tx.Update(ctx, Attempts, attempt.ID, changed(map[string]any{"status": "PAID"})); err != nil {
				return "", err
			}
			outcome = "APPLIED"
		}
	}
	return outcome, tx.Update(ctx, PaymentEvents, record.ID, changed(map[string]any{"outcome": outcome, "processed_at": now}))
}
func (s *Service) ReconcileEvents(ctx context.Context) error {
	var records []domain.PaymentEvent
	if err := s.Store.Find(ctx, PaymentEvents, Query{Eq: map[string]any{"processed_at": nil}, Sort: "updated_at", Limit: 20}, &records); err != nil {
		return err
	}
	for _, record := range records {
		if err := s.Store.Transaction(ctx, func(tx Store) error {
			if err := tx.LockKey(ctx, "webhook:"+record.DedupeKey); err != nil {
				return err
			}
			current, err := findOne[domain.PaymentEvent](ctx, tx, PaymentEvents, byID(record.ID))
			if err != nil {
				return err
			}
			if current.ProcessedAt != nil {
				return nil
			}
			_, err = s.processPaymentEvent(ctx, tx, current)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) SimulatePayment(ctx context.Context, bill, verified string) error {
	if s.Options.Production || s.Payments.Name() != "dev" {
		return domain.Fail("NOT_FOUND", 404)
	}
	order, err := findOne[domain.Order](ctx, s.Store, Orders, Query{Eq: map[string]any{"bill_number": bill}})
	if err != nil {
		return err
	}
	if verified == "" || verified != order.RecipientPhone {
		return domain.Fail("FORBIDDEN", 403)
	}
	attempt, err := findOne[domain.Attempt](ctx, s.Store, Attempts, Query{Eq: map[string]any{"order_id": bill, "status": "CREATED"}, Sort: "created_at", Desc: true})
	if err != nil {
		return err
	}
	if attempt.ProviderTransactionID == nil {
		return domain.Fail("INVALID_STATE", 409)
	}
	raw := []byte(fmt.Sprintf(`{"transactionId":%q,"billNumber":%q,"txnAmount":%d,"status":"PAYMENT_COMPLETED","refNo":"simulation"}`, *attempt.ProviderTransactionID, bill, order.TotalKip))
	_, err = s.Webhook(ctx, raw)
	return err
}
