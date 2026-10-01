package application

import (
	"context"
	"errors"
	"fmt"
	"shopnext-laos/internal/domain"
	"strings"
	"time"
)

func (s *Service) RequestRefund(ctx context.Context, a domain.Actor, bill, reason, ip string) (domain.Refund, error) {
	var result domain.Refund
	if err := Require(a, "refunds.request"); err != nil {
		return result, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 2000 {
		return result, domain.Fail("INVALID_REFUND_REASON", 400)
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": bill}, Lock: true})
		if err != nil {
			return err
		}
		if order.PaymentType != "ONLINE" {
			return domain.Fail("NOT_ONLINE", 409)
		}
		if order.PaymentStatus != "PAID" {
			return domain.Fail("NOT_PAID", 409)
		}
		n, err := tx.Count(ctx, Refunds, Query{Eq: map[string]any{"order_id": bill}, In: map[string][]string{"status": {"REQUESTED", "APPROVED", "SUBMITTED", "PROCESSING", "UNKNOWN", "MANUAL_REVIEW"}}})
		if err != nil {
			return err
		}
		if n > 0 {
			return domain.Fail("ACTIVE_REFUND_EXISTS", 409)
		}
		attempt, err := findOne[domain.Attempt](ctx, tx, Attempts, Query{Eq: map[string]any{"order_id": bill, "status": "PAID"}, Sort: "created_at", Desc: true})
		if err != nil {
			return domain.Fail("NO_PAID_ATTEMPT", 409)
		}
		result = domain.Refund{Base: domain.NewBase(), OrderID: bill, PaymentAttemptID: attempt.ID, AmountKip: order.TotalKip, Reason: reason, Status: "REQUESTED", RequestedBy: a.Email}
		if err := tx.Insert(ctx, Refunds, &result); err != nil {
			return err
		}
		if err := event(ctx, tx, bill, "REFUND_REQUESTED", reason); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "refund.request", "refund", result.ID, reason, ip)
	})
	return result, err
}
func (s *Service) ApproveRefund(ctx context.Context, a domain.Actor, id, ip string) error {
	if err := Require(a, "refunds.approve"); err != nil {
		return err
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		refund, err := findOne[domain.Refund](ctx, tx, Refunds, byID(id))
		if err != nil {
			return err
		}
		if _, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": refund.OrderID}, Lock: true}); err != nil {
			return err
		}
		refund, err = findOne[domain.Refund](ctx, tx, Refunds, locked(byID(id)))
		if err != nil {
			return err
		}
		if refund.Status != "REQUESTED" {
			return domain.Fail("INVALID_STATE", 409)
		}
		if !a.Legacy && refund.RequestedBy == a.Email {
			return domain.Fail("SELF_APPROVAL", 403)
		}
		attempt, err := findOne[domain.Attempt](ctx, tx, Attempts, byID(refund.PaymentAttemptID))
		if err != nil {
			return err
		}
		if attempt.ProviderTransactionID == nil {
			return domain.Fail("NO_PROVIDER_TRANSACTION", 409)
		}
		if err := tx.Update(ctx, Refunds, id, changed(map[string]any{"status": "APPROVED", "approved_by": a.Email})); err != nil {
			return err
		}
		if err := event(ctx, tx, refund.OrderID, "REFUND_APPROVED", a.Email); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "refund.approve", "refund", id, "", ip)
	})
	if err != nil {
		return err
	}
	return s.submitApprovedRefund(ctx, id)
}
func (s *Service) submitApprovedRefund(ctx context.Context, id string) error {
	var refund domain.Refund
	var transaction string
	claimed := false
	err := s.Store.Transaction(ctx, func(tx Store) error {
		var err error
		refund, err = findOne[domain.Refund](ctx, tx, Refunds, locked(byID(id)))
		if err != nil {
			return err
		}
		if refund.Status != "APPROVED" {
			return nil
		}
		attempt, err := findOne[domain.Attempt](ctx, tx, Attempts, byID(refund.PaymentAttemptID))
		if err != nil {
			return err
		}
		if attempt.ProviderTransactionID == nil {
			return domain.Fail("NO_PROVIDER_TRANSACTION", 409)
		}
		transaction = *attempt.ProviderTransactionID
		claimed = true
		return tx.Update(ctx, Refunds, id, changed(map[string]any{"status": "UNKNOWN", "submitted_at": time.Now().UTC(), "last_error": "submission in progress; never resubmit automatically"}))
	})
	if err != nil || !claimed {
		return err
	}
	result, providerErr := s.Payments.SubmitRefund(ctx, transaction)
	return s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": refund.OrderID}, Lock: true}); err != nil {
			return err
		}
		current, err := findOne[domain.Refund](ctx, tx, Refunds, locked(byID(id)))
		if err != nil {
			return err
		}
		if current.Status != "UNKNOWN" {
			return nil
		}
		values := map[string]any{}
		action := "REFUND_SUBMITTED"
		if providerErr != nil {
			var ambiguous *AmbiguousError
			if errors.As(providerErr, &ambiguous) {
				values["status"] = "UNKNOWN"
				values["last_error"] = "provider outcome unknown; reconcile before retry"
				action = "REFUND_UNKNOWN"
			} else {
				values["status"] = "REJECTED"
				values["last_error"] = "provider rejected refund"
				action = "REFUND_REJECTED"
			}
		} else {
			values["status"] = "SUBMITTED"
			values["provider_refund_bill_id"] = result.ID
			values["provider_status"] = result.Status
			values["last_error"] = ""
		}
		if err := tx.Update(ctx, Refunds, id, changed(values)); err != nil {
			return err
		}
		return event(ctx, tx, refund.OrderID, action, "")
	})
}
func (s *Service) completeRefund(ctx context.Context, tx Store, refund domain.Refund, providerID, status string, success bool) error {
	order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": refund.OrderID}, Lock: true})
	if err != nil {
		return err
	}
	current, err := findOne[domain.Refund](ctx, tx, Refunds, locked(byID(refund.ID)))
	if err != nil {
		return err
	}
	if current.Status == "SUCCEEDED" || current.Status == "FAILED" || current.Status == "REJECTED" {
		return nil
	}
	values := map[string]any{"provider_refund_bill_id": providerID, "provider_status": status}
	action := "REFUND_FAILED"
	values["status"] = "FAILED"
	if success {
		values["status"] = "SUCCEEDED"
		values["completed_at"] = time.Now().UTC()
		action = "REFUND_SUCCEEDED"
		if order.PaymentStatus != "PAID" && order.PaymentStatus != "REFUNDED" {
			return domain.Fail("INVALID_STATE", 409)
		}
		if err := tx.Update(ctx, Orders, order.BillNumber, map[string]any{"payment_status": "REFUNDED"}); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, "sms:refund:"+refund.ID, order.RecipientPhone, fmt.Sprintf("ShopNext Laos: ຄືນເງິນບິນ %s ຈຳນວນ %d ກີບ ສຳເລັດແລ້ວ", order.BillNumber, refund.AmountKip)); err != nil {
			return err
		}
	}
	if err := tx.Update(ctx, Refunds, refund.ID, changed(values)); err != nil {
		return err
	}
	return event(ctx, tx, refund.OrderID, action, status)
}
func (s *Service) ResolveRefund(ctx context.Context, a domain.Actor, id, providerID string, success bool, ip string) error {
	if err := Require(a, "refunds.resolve"); err != nil {
		return err
	}
	if providerID == "" || len(providerID) > 200 {
		return domain.Fail("INVALID_PROVIDER_REFUND_ID", 400)
	}
	return s.Store.Transaction(ctx, func(tx Store) error {
		refund, err := findOne[domain.Refund](ctx, tx, Refunds, byID(id))
		if err != nil {
			return err
		}
		if _, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": refund.OrderID}, Lock: true}); err != nil {
			return err
		}
		refund, err = findOne[domain.Refund](ctx, tx, Refunds, locked(byID(id)))
		if err != nil {
			return err
		}
		if refund.Status != "MANUAL_REVIEW" && refund.Status != "UNKNOWN" {
			return domain.Fail("INVALID_STATE", 409)
		}
		if refund.SubmittedAt != nil && refund.SubmittedAt.Add(30*time.Second).After(time.Now()) {
			return domain.Fail("REFUND_SUBMISSION_IN_PROGRESS", 409)
		}
		if err := s.completeRefund(ctx, tx, refund, providerID, "MANUALLY_VERIFIED", success); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "refund.resolve", "refund", id, providerID, ip)
	})
}
func (s *Service) PollRefunds(ctx context.Context) error {
	var refunds []domain.Refund
	if err := s.Store.Find(ctx, Refunds, Query{In: map[string][]string{"status": {"APPROVED", "SUBMITTED", "PROCESSING", "UNKNOWN"}}, Sort: "updated_at", Limit: 20}, &refunds); err != nil {
		return err
	}
	for _, refund := range refunds {
		if refund.Status == "APPROVED" {
			if err := s.submitApprovedRefund(ctx, refund.ID); err != nil {
				return err
			}
			continue
		}
		if refund.ProviderRefundBillID == nil {
			if refund.SubmittedAt != nil && refund.SubmittedAt.Add(30*time.Second).Before(time.Now()) {
				if err := s.Store.Transaction(ctx, func(tx Store) error {
					r, err := findOne[domain.Refund](ctx, tx, Refunds, locked(byID(refund.ID)))
					if err != nil {
						return err
					}
					if r.Status != "UNKNOWN" || r.ProviderRefundBillID != nil {
						return nil
					}
					return tx.Update(ctx, Refunds, r.ID, changed(map[string]any{"status": "MANUAL_REVIEW", "last_error": "no provider refund ID; reconcile in portal"}))
				}); err != nil {
					return err
				}
			}
			continue
		}
		result, providerErr := s.Payments.RefundStatus(ctx, *refund.ProviderRefundBillID)
		if providerErr != nil && (refund.SubmittedAt == nil || refund.SubmittedAt.Add(24*time.Hour).After(time.Now())) {
			continue
		}
		if err := s.Store.Transaction(ctx, func(tx Store) error {
			if _, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": refund.OrderID}, Lock: true}); err != nil {
				return err
			}
			current, err := findOne[domain.Refund](ctx, tx, Refunds, locked(byID(refund.ID)))
			if err != nil {
				return err
			}
			if current.Status != "SUBMITTED" && current.Status != "PROCESSING" && current.Status != "UNKNOWN" {
				return nil
			}
			if result.Amount != nil && *result.Amount != current.AmountKip {
				return tx.Update(ctx, Refunds, current.ID, changed(map[string]any{"status": "MANUAL_REVIEW", "last_error": "refund amount mismatch"}))
			}
			if result.Success || result.Failed {
				return s.completeRefund(ctx, tx, current, *current.ProviderRefundBillID, result.Status, result.Success)
			}
			state := "PROCESSING"
			lastError := ""
			if current.SubmittedAt != nil && current.SubmittedAt.Add(24*time.Hour).Before(time.Now()) {
				state = "MANUAL_REVIEW"
				lastError = "24h refund deadline exceeded"
			}
			return tx.Update(ctx, Refunds, current.ID, changed(map[string]any{"status": state, "provider_status": result.Status, "last_error": lastError}))
		}); err != nil {
			return err
		}
	}
	return nil
}
