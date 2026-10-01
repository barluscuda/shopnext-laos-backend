package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"shopnext-laos/internal/domain"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type CheckoutItem struct {
	VariantID string `json:"variant_id"`
	Quantity  int64  `json:"quantity"`
}
type Checkout struct {
	RecipientName  string         `json:"recipient_name"`
	RecipientPhone string         `json:"recipient_phone"`
	ProviderID     string         `json:"provider_id"`
	Province       string         `json:"province"`
	City           string         `json:"city"`
	BranchName     string         `json:"branch_name"`
	PaymentType    string         `json:"payment_type"`
	PaymentMethod  string         `json:"payment_method"`
	Items          []CheckoutItem `json:"items"`
}
type CheckoutResult struct {
	BillNumber string `json:"bill_number"`
	PickupCode string `json:"pickup_code"`
	TotalKip   int64  `json:"total_kip"`
}

func event(ctx context.Context, tx Store, bill, action, note string) error {
	return tx.Insert(ctx, OrderEvents, &domain.OrderEvent{Base: domain.NewBase(), OrderID: bill, Action: action, Note: note})
}
func enqueue(ctx context.Context, tx Store, key, phone, message string) error {
	n, err := tx.Count(ctx, OutboxEvents, Query{Eq: map[string]any{"dedupe_key": key}})
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return tx.Insert(ctx, OutboxEvents, &domain.Outbox{Base: domain.NewBase(), Phone: phone, Message: message, DedupeKey: key, Status: "PENDING", AvailableAt: time.Now().UTC()})
}
func (s *Service) Checkout(ctx context.Context, in Checkout, verified, key, ip string) (CheckoutResult, error) {
	var result CheckoutResult
	in.RecipientPhone = domain.NormalizePhone(in.RecipientPhone)
	in.RecipientName = strings.TrimSpace(in.RecipientName)
	in.BranchName = strings.TrimSpace(in.BranchName)
	if !domain.ValidPhone(in.RecipientPhone) || utf8.RuneCountInString(in.RecipientName) < 2 || utf8.RuneCountInString(in.RecipientName) > 80 || utf8.RuneCountInString(in.BranchName) < 2 || utf8.RuneCountInString(in.BranchName) > 80 || len(in.Items) < 1 || len(in.Items) > 30 || len(key) > 128 {
		return result, domain.Fail("INVALID_CHECKOUT", 400)
	}
	if verified == "" || verified != in.RecipientPhone {
		return result, domain.Fail("PHONE_VERIFICATION_REQUIRED", 403)
	}
	if !ValidDistrict(in.Province, in.City) {
		return result, domain.Fail("INVALID_DELIVERY_GEOGRAPHY", 400)
	}
	if in.PaymentType != "COD_PROVIDER" && in.PaymentType != "ONLINE" {
		return result, domain.Fail("INVALID_PAYMENT_TYPE", 400)
	}
	if in.PaymentType == "ONLINE" {
		if _, ok := domain.Banks[in.PaymentMethod]; !ok {
			return result, domain.Fail("UNSUPPORTED_BANK", 400)
		}
	} else {
		in.PaymentMethod = ""
	}
	quantities := map[string]int64{}
	for _, item := range in.Items {
		if item.VariantID == "" || item.Quantity < 1 || item.Quantity > 99 {
			return result, domain.Fail("INVALID_ITEMS", 400)
		}
		quantities[item.VariantID] += item.Quantity
		if quantities[item.VariantID] > 99 {
			return result, domain.Fail("INVALID_ITEMS", 400)
		}
	}
	ids := make([]string, 0, len(quantities))
	for id := range quantities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	in.Items = nil
	for _, id := range ids {
		in.Items = append(in.Items, CheckoutItem{id, quantities[id]})
	}
	body, _ := json.Marshal(in)
	hash := domain.Hash(string(body))
	scope := "orders:" + verified
	created := false
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if key != "" {
			if err := tx.LockKey(ctx, "idem:"+scope+":"+key); err != nil {
				return err
			}
			saved, err := findOne[domain.Idempotency](ctx, tx, Idempotencies, Query{Eq: map[string]any{"scope": scope, "key": key}})
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			if err == nil && saved.ExpiresAt.After(time.Now()) {
				if saved.RequestHash != hash {
					return domain.Fail("IDEMPOTENCY_CONFLICT", 409)
				}
				return json.Unmarshal(saved.Response, &result)
			}
			if err == nil {
				if err := tx.Delete(ctx, Idempotencies, saved.ID); err != nil {
					return err
				}
			}
		}
		if err := s.Rate(ctx, "order-create-phone", verified, 3, 10*time.Minute); err != nil {
			return err
		}
		if err := s.Rate(ctx, "order-create-ip", ip, 10, 10*time.Minute); err != nil {
			return err
		}
		provider, err := findOne[domain.Provider](ctx, tx, Providers, locked(byID(in.ProviderID)))
		if err != nil {
			return err
		}
		branchKey := in.ProviderID + ":" + in.Province + ":" + in.City + ":" + in.BranchName
		if err := tx.LockKey(ctx, "branch:"+branchKey); err != nil {
			return err
		}
		branch, err := findOne[domain.Branch](ctx, tx, Branches, Query{Eq: map[string]any{"provider_id": in.ProviderID, "province": in.Province, "city": in.City, "name": in.BranchName}})
		if errors.Is(err, domain.ErrNotFound) {
			branch = domain.Branch{Base: domain.NewBase(), ProviderID: in.ProviderID, Province: in.Province, City: in.City, Name: in.BranchName}
			err = tx.Insert(ctx, Branches, &branch)
		}
		if err != nil {
			return err
		}
		var subtotal int64
		items := make([]domain.Item, 0, len(ids))
		// All catalog writers lock products before variants. Match that order,
		// and sort both sets to avoid cross-product checkout deadlocks.
		productIDs := map[string]bool{}
		variantProducts := map[string]string{}
		for _, id := range ids {
			v, err := findOne[domain.Variant](ctx, tx, Variants, byID(id))
			if err != nil {
				return err
			}
			variantProducts[id] = v.ProductID
			productIDs[v.ProductID] = true
		}
		orderedProducts := make([]string, 0, len(productIDs))
		for id := range productIDs {
			orderedProducts = append(orderedProducts, id)
		}
		sort.Strings(orderedProducts)
		products := map[string]domain.Product{}
		for _, id := range orderedProducts {
			p, err := findOne[domain.Product](ctx, tx, Products, locked(byID(id)))
			if err != nil {
				return err
			}
			products[id] = p
		}
		for _, id := range ids {
			variant, err := findOne[domain.Variant](ctx, tx, Variants, locked(byID(id)))
			if err != nil {
				return err
			}
			if variant.ProductID != variantProducts[id] {
				return domain.Fail("STOCK_CONFLICT", 409)
			}
			product := products[variant.ProductID]
			if product.Status != "ACTIVE" || product.DeletedAt != nil {
				return domain.Fail("PRODUCT_UNAVAILABLE", 409)
			}
			quantity := quantities[id]
			if variant.Available() < quantity {
				return domain.Fail("STOCK_CONFLICT", 409)
			}
			unit := product.BasePriceKip
			if variant.PriceOverrideKip != nil {
				unit = *variant.PriceOverrideKip
			}
			line, err := domain.LineTotal(unit, quantity)
			if err != nil {
				return err
			}
			subtotal, err = domain.AddMoney(subtotal, line)
			if err != nil {
				return err
			}
			values := map[string]any{}
			if in.PaymentType == "ONLINE" {
				values["stock_reserved"] = variant.StockReserved + quantity
			} else {
				values["stock_on_hand"] = variant.StockOnHand - quantity
			}
			if err := tx.Update(ctx, Variants, id, changed(values)); err != nil {
				return err
			}
			items = append(items, domain.Item{Base: domain.NewBase(), ProductVariantID: id, ProductNameSnapshot: product.Name, UnitPriceKipSnapshot: unit, Quantity: quantity, LineTotalKip: line})
		}
		total, err := domain.AddMoney(subtotal, provider.ShippingFeeKip)
		if err != nil {
			return err
		}
		bill := domain.BillNumber()
		pickup := domain.Digits()
		for retries := 0; ; retries++ {
			if retries == 20 {
				return domain.Fail("IDENTIFIER_CAPACITY_EXCEEDED", 503)
			}
			if err := tx.LockKey(ctx, "pickup:"+pickup); err != nil {
				return err
			}
			n, err := tx.Count(ctx, Orders, Query{Eq: map[string]any{"pickup_code": pickup}})
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}
			pickup = domain.Digits()
		}
		now := time.Now().UTC()
		order := domain.Order{BillNumber: bill, PickupCode: pickup, RecipientPhone: verified, RecipientName: in.RecipientName, ExpressProviderID: provider.ID, BranchID: branch.ID, PaymentType: in.PaymentType, PaymentMethod: in.PaymentMethod, PaymentStatus: "PENDING", OrderStatus: "CONFIRMED", SubtotalKip: subtotal, ShippingFeeKip: provider.ShippingFeeKip, TotalKip: total, VerifiedAt: now, CreatedAt: now}
		if in.PaymentType == "ONLINE" {
			expiry := now.Add(s.Options.Hold)
			order.ReservationExpiresAt = &expiry
			order.OrderStatus = "PENDING_PAYMENT"
		}
		if err := tx.Insert(ctx, Orders, &order); err != nil {
			return err
		}
		for i := range items {
			items[i].OrderID = bill
			if err := tx.Insert(ctx, Items, &items[i]); err != nil {
				return err
			}
		}
		if err := event(ctx, tx, bill, "CREATED", in.PaymentType); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, "sms:created:"+bill, verified, fmt.Sprintf("ShopNext Laos: ບິນ %s ລະຫັດຮັບ %s ລວມ %d ກີບ", bill, pickup, total)); err != nil {
			return err
		}
		result = CheckoutResult{bill, pickup, total}
		if key != "" {
			response, _ := json.Marshal(result)
			if err := tx.Insert(ctx, Idempotencies, &domain.Idempotency{Base: domain.NewBase(), Scope: scope, Key: key, RequestHash: hash, Response: response, ExpiresAt: now.Add(24 * time.Hour)}); err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return result, err
	}
	if created && in.PaymentType == "ONLINE" {
		_, _ = s.CreateAttempt(ctx, result.BillNumber, in.PaymentMethod, verified)
	}
	return result, nil
}

func (s *Service) transition(ctx context.Context, tx Store, order domain.Order, action, note string) error {
	now := time.Now().UTC()
	if action == "PAID" {
		if order.OrderStatus != "PENDING_PAYMENT" {
			return domain.Fail("INVALID_STATE", 409)
		}
		if order.ReservationExpiresAt == nil || !order.ReservationExpiresAt.After(now) {
			return domain.Fail("RESERVATION_EXPIRED", 409)
		}
	} else if action == "DELIVERED" {
		if order.OrderStatus != "CONFIRMED" {
			return domain.Fail("INVALID_STATE", 409)
		}
	} else if action == "CANCELLED" {
		if order.OrderStatus != "PENDING_PAYMENT" && order.OrderStatus != "CONFIRMED" {
			return domain.Fail("INVALID_STATE", 409)
		}
	} else if action == "EXPIRED" {
		if order.OrderStatus != "PENDING_PAYMENT" || order.ReservationExpiresAt == nil || order.ReservationExpiresAt.After(now) {
			return domain.Fail("INVALID_STATE", 409)
		}
	} else {
		return domain.Fail("INVALID_ACTION", 400)
	}
	if action != "DELIVERED" {
		var items []domain.Item
		if err := tx.Find(ctx, Items, Query{Eq: map[string]any{"order_id": order.BillNumber}, Sort: "product_variant_id"}, &items); err != nil {
			return err
		}
		for _, item := range items {
			variant, err := findOne[domain.Variant](ctx, tx, Variants, locked(byID(item.ProductVariantID)))
			if err != nil {
				return err
			}
			values := map[string]any{}
			switch {
			case action == "PAID":
				if variant.StockReserved < item.Quantity || variant.StockOnHand < item.Quantity {
					return domain.Fail("STOCK_CONFLICT", 409)
				}
				values["stock_on_hand"] = variant.StockOnHand - item.Quantity
				values["stock_reserved"] = variant.StockReserved - item.Quantity
			case order.OrderStatus == "PENDING_PAYMENT":
				if variant.StockReserved < item.Quantity {
					return domain.Fail("STOCK_CONFLICT", 409)
				}
				values["stock_reserved"] = variant.StockReserved - item.Quantity
			default:
				sum, err := domain.AddMoney(variant.StockOnHand, item.Quantity)
				if err != nil {
					return err
				}
				values["stock_on_hand"] = sum
			}
			if err := tx.Update(ctx, Variants, variant.ID, changed(values)); err != nil {
				return err
			}
		}
	}
	values := map[string]any{"order_status": action, "reservation_expires_at": nil}
	if action == "PAID" {
		values["order_status"] = "CONFIRMED"
		values["payment_status"] = "PAID"
	}
	if err := tx.Update(ctx, Orders, order.BillNumber, values); err != nil {
		return err
	}
	if action == "CANCELLED" || action == "EXPIRED" {
		var attempts []domain.Attempt
		if err := tx.Find(ctx, Attempts, Query{Eq: map[string]any{"order_id": order.BillNumber}, In: map[string][]string{"status": {"PENDING", "CREATED"}}}, &attempts); err != nil {
			return err
		}
		for _, attempt := range attempts {
			state := "FAILED"
			if action == "EXPIRED" {
				state = "EXPIRED"
			}
			if err := tx.Update(ctx, Attempts, attempt.ID, changed(map[string]any{"status": state, "last_error": "order " + strings.ToLower(action)})); err != nil {
				return err
			}
		}
	}
	if err := event(ctx, tx, order.BillNumber, action, note); err != nil {
		return err
	}
	if action == "PAID" {
		return enqueue(ctx, tx, "sms:paid:"+order.BillNumber, order.RecipientPhone, fmt.Sprintf("ShopNext Laos: ບິນ %s ຊຳລະສຳເລັດ ລະຫັດຮັບ %s", order.BillNumber, order.PickupCode))
	}
	return nil
}
func (s *Service) OrderAction(ctx context.Context, a domain.Actor, bill, action, note, ip string) error {
	permission := "orders.act"
	if action == "CANCELLED" {
		permission = "orders.cancel"
	}
	if err := Require(a, permission); err != nil {
		return err
	}
	return s.Store.Transaction(ctx, func(tx Store) error {
		order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": bill}, Lock: true})
		if err != nil {
			return err
		}
		if err := s.transition(ctx, tx, order, action, note); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "order."+strings.ToLower(action), "order", bill, note, ip)
	})
}
func (s *Service) Expire(ctx context.Context, onlyBill string) (int, error) {
	count := 0
	var orders []domain.Order
	q := Query{Eq: map[string]any{"order_status": "PENDING_PAYMENT"}, LT: map[string]any{"reservation_expires_at": time.Now()}, Sort: "created_at", Limit: 100}
	if onlyBill != "" {
		q.Eq["bill_number"] = onlyBill
	}
	if err := s.Store.Find(ctx, Orders, q, &orders); err != nil {
		return 0, err
	}
	for _, order := range orders {
		err := s.Store.Transaction(ctx, func(tx Store) error {
			current, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": order.BillNumber}, Lock: true})
			if err != nil {
				return err
			}
			if current.OrderStatus != "PENDING_PAYMENT" || current.ReservationExpiresAt == nil || current.ReservationExpiresAt.After(time.Now()) {
				return nil
			}
			if err := s.transition(ctx, tx, current, "EXPIRED", "payment hold expired"); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

type Bill struct {
	domain.Order
	RecipientNameMasked bool                `json:"recipient_name_masked"`
	Items               []domain.Item       `json:"items"`
	Provider            domain.Provider     `json:"provider"`
	Branch              domain.Branch       `json:"branch"`
	PaymentAttempt      *domain.Attempt     `json:"payment_attempt"`
	Events              []domain.OrderEvent `json:"events,omitempty"`
	Refunds             []domain.Refund     `json:"refunds,omitempty"`
}

func (s *Service) Bill(ctx context.Context, bill, verified string, admin bool) (Bill, error) {
	var result Bill
	if _, err := s.Expire(ctx, bill); err != nil {
		return result, err
	}
	order, err := findOne[domain.Order](ctx, s.Store, Orders, Query{Eq: map[string]any{"bill_number": bill}})
	if err != nil {
		return result, err
	}
	result.Order = order
	owner := verified != "" && verified == order.RecipientPhone
	if !owner && !admin {
		result.RecipientName = domain.MaskName(order.RecipientName)
		result.RecipientNameMasked = true
	}
	if err := s.Store.Find(ctx, Items, Query{Eq: map[string]any{"order_id": bill}, Sort: "created_at"}, &result.Items); err != nil {
		return result, err
	}
	result.Provider, err = findOne[domain.Provider](ctx, s.Store, Providers, byID(order.ExpressProviderID))
	if err != nil {
		return result, err
	}
	result.Branch, err = findOne[domain.Branch](ctx, s.Store, Branches, byID(order.BranchID))
	if err != nil {
		return result, err
	}
	if (owner || admin) && order.OrderStatus == "PENDING_PAYMENT" {
		attempt, err := findOne[domain.Attempt](ctx, s.Store, Attempts, Query{Eq: map[string]any{"order_id": bill}, In: map[string][]string{"status": {"PENDING", "CREATED"}}, Sort: "created_at", Desc: true})
		if err == nil {
			result.PaymentAttempt = &attempt
		} else if !errors.Is(err, domain.ErrNotFound) {
			return result, err
		}
	}
	if admin {
		if err := s.Store.Find(ctx, OrderEvents, Query{Eq: map[string]any{"order_id": bill}, Sort: "created_at"}, &result.Events); err != nil {
			return result, err
		}
		err = s.Store.Find(ctx, Refunds, Query{Eq: map[string]any{"order_id": bill}, Sort: "created_at", Desc: true}, &result.Refunds)
	}
	return result, err
}
func (s *Service) Bills(ctx context.Context, phone, verified string) ([]domain.Order, error) {
	phone = domain.NormalizePhone(phone)
	if verified == "" || phone != verified {
		return nil, domain.Fail("PHONE_VERIFICATION_REQUIRED", 403)
	}
	var orders []domain.Order
	err := s.Store.Find(ctx, Orders, Query{Eq: map[string]any{"recipient_phone": phone}, Sort: "created_at", Desc: true, Limit: 50}, &orders)
	return orders, err
}
