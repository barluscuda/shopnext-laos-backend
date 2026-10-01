package application

import (
	"context"
	"errors"
	"fmt"
	"shopnext-laos/internal/domain"
	"time"
)

type Worker struct {
	Service             *Service
	consecutiveFailures int
	breakerUntil        time.Time
}

func (w *Worker) DrainOutbox(ctx context.Context) (int, error) {
	if time.Now().Before(w.breakerUntil) {
		return 0, nil
	}
	var claimed []domain.Outbox
	s := w.Service
	now := time.Now().UTC()
	err := s.Store.Transaction(ctx, func(tx Store) error {
		var abandoned []domain.Outbox
		if err := tx.Find(ctx, OutboxEvents, Query{Eq: map[string]any{"status": "SENDING"}, LT: map[string]any{"lease_until": now}, Limit: 20, Lock: true, SkipLocked: true}, &abandoned); err != nil {
			return err
		}
		for _, e := range abandoned {
			state := "FAILED"
			if e.Attempts >= 8 {
				state = "DEAD"
			}
			if err := tx.Update(ctx, OutboxEvents, e.ID, changed(map[string]any{"status": state, "available_at": now, "lease_until": nil, "last_error": "worker lease expired; gateway acceptance may be ambiguous"})); err != nil {
				return err
			}
		}
		if err := tx.Find(ctx, OutboxEvents, Query{In: map[string][]string{"status": {"PENDING", "FAILED"}}, LT: map[string]any{"available_at": now.Add(time.Nanosecond)}, Sort: "available_at", Limit: 20, Lock: true, SkipLocked: true}, &claimed); err != nil {
			return err
		}
		for i, e := range claimed {
			lease := now.Add(5 * time.Minute)
			claimed[i].Attempts = e.Attempts + 1
			if err := tx.Update(ctx, OutboxEvents, e.ID, changed(map[string]any{"status": "SENDING", "attempts": e.Attempts + 1, "lease_until": lease})); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	sent := 0
	failures := 0
	for _, e := range claimed {
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		accepted, sendErr := s.SMS.Send(ctx, e.Phone, e.Message)
		err := s.Store.Transaction(ctx, func(tx Store) error {
			current, err := findOne[domain.Outbox](ctx, tx, OutboxEvents, locked(byID(e.ID)))
			if err != nil {
				return err
			}
			if current.Status != "SENDING" || current.Attempts != e.Attempts {
				return nil
			}
			values := map[string]any{"lease_until": nil}
			if sendErr == nil && accepted {
				values["status"] = "SENT"
				values["sent_at"] = time.Now().UTC()
				values["last_error"] = ""
				if err := tx.Insert(ctx, SMSLogs, &domain.SMSLog{Base: domain.NewBase(), Phone: e.Phone, Message: e.Message, Accepted: true}); err != nil {
					return err
				}
				sent++
			} else {
				failures++
				state := "FAILED"
				if e.Attempts >= 8 {
					state = "DEAD"
				}
				delay := 30 * time.Second * time.Duration(1<<min(e.Attempts-1, 7))
				if delay > time.Hour {
					delay = time.Hour
				}
				values["status"] = state
				values["last_error"] = "SMS gateway did not confirm acceptance"
				values["available_at"] = time.Now().Add(delay)
			}
			return tx.Update(ctx, OutboxEvents, e.ID, changed(values))
		})
		if err != nil {
			return sent, err
		}
	}
	if failures > 0 {
		w.consecutiveFailures++
		if w.consecutiveFailures >= 5 {
			w.breakerUntil = time.Now().Add(5 * time.Minute)
			w.consecutiveFailures = 0
		}
	} else if len(claimed) > 0 {
		w.consecutiveFailures = 0
	}
	return sent, nil
}
func (s *Service) ExpireAttempts(ctx context.Context) error {
	var attempts []domain.Attempt
	if err := s.Store.Find(ctx, Attempts, Query{In: map[string][]string{"status": {"PENDING", "CREATED"}}, LT: map[string]any{"expires_at": time.Now()}, Limit: 100}, &attempts); err != nil {
		return err
	}
	for _, a := range attempts {
		if err := s.Store.Transaction(ctx, func(tx Store) error {
			if _, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": a.OrderID}, Lock: true}); err != nil {
				return err
			}
			current, err := findOne[domain.Attempt](ctx, tx, Attempts, locked(byID(a.ID)))
			if err != nil {
				return err
			}
			if current.Status != "PENDING" && current.Status != "CREATED" {
				return nil
			}
			return tx.Update(ctx, Attempts, a.ID, changed(map[string]any{"status": "EXPIRED"}))
		}); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) CleanupIdempotency(ctx context.Context) error {
	var expired []domain.Idempotency
	if err := s.Store.Find(ctx, Idempotencies, Query{LT: map[string]any{"expires_at": time.Now()}, Limit: 500}, &expired); err != nil {
		return err
	}
	for _, record := range expired {
		if err := s.Store.Transaction(ctx, func(tx Store) error {
			if err := tx.LockKey(ctx, "idem:"+record.Scope+":"+record.Key); err != nil {
				return err
			}
			current, err := findOne[domain.Idempotency](ctx, tx, Idempotencies, byID(record.ID))
			if errors.Is(err, domain.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if current.ExpiresAt.After(time.Now()) {
				return nil
			}
			return tx.Delete(ctx, Idempotencies, current.ID)
		}); err != nil {
			return err
		}
	}
	return nil
}
func (w *Worker) Tick(ctx context.Context) error {
	var failures []error
	s := w.Service
	if _, err := s.Expire(ctx, ""); err != nil {
		failures = append(failures, err)
	}
	for _, fn := range []func(context.Context) error{s.ExpireAttempts, s.ReconcileEvents, s.PollRefunds} {
		if err := fn(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	if _, err := w.DrainOutbox(ctx); err != nil {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		return fmt.Errorf("worker tick: %v", failures)
	}
	return nil
}
