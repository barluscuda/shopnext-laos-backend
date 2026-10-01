package application

import (
	"context"
	"shopnext-laos/internal/domain"
	"time"
)

func ResourcePermission(e Entity) string {
	switch e {
	case Products, Variants, Images:
		return "products.manage"
	case Categories, Providers, Branches:
		return "taxonomy.manage"
	case HeroSlides, PromoBanners:
		return "content.manage"
	case Orders, Attempts, PaymentEvents, Refunds, OrderEvents:
		return "orders.view"
	case StaffUsers:
		return "staff.manage"
	case SMSLogs, OutboxEvents:
		return "sms.view"
	case Audits:
		return "staff.manage"
	}
	return ""
}
func (s *Service) AdminList(ctx context.Context, a domain.Actor, e Entity, q Query) (any, int64, error) {
	permission := ResourcePermission(e)
	if permission == "" {
		return nil, 0, domain.Fail("INVALID_RESOURCE", 400)
	}
	if err := Require(a, permission); err != nil {
		return nil, 0, err
	}
	total, err := s.Store.Count(ctx, e, q)
	if err != nil {
		return nil, 0, err
	}
	var output any
	switch e {
	case Products:
		output = &[]domain.Product{}
	case Variants:
		output = &[]domain.Variant{}
	case Images:
		output = &[]domain.Image{}
	case Categories:
		output = &[]domain.Category{}
	case Providers:
		output = &[]domain.Provider{}
	case Branches:
		output = &[]domain.Branch{}
	case HeroSlides, PromoBanners:
		output = &[]domain.Content{}
	case Orders:
		output = &[]domain.Order{}
	case Attempts:
		output = &[]domain.Attempt{}
	case PaymentEvents:
		output = &[]domain.PaymentEvent{}
	case Refunds:
		output = &[]domain.Refund{}
	case StaffUsers:
		output = &[]domain.Staff{}
	case SMSLogs:
		output = &[]domain.SMSLog{}
	case OutboxEvents:
		output = &[]domain.Outbox{}
	case Audits:
		output = &[]domain.Audit{}
	case OrderEvents:
		output = &[]domain.OrderEvent{}
	}
	err = s.Store.Find(ctx, e, q, output)
	return output, total, err
}

type LowStock struct {
	domain.Variant
	ProductName string `json:"product_name"`
	ProductSlug string `json:"product_slug"`
}
type SalesPoint struct {
	Date    string `json:"date"`
	Revenue int64  `json:"revenue_kip"`
	Items   int64  `json:"items"`
}
type Dashboard struct {
	StatusCounts  map[string]int64 `json:"status_counts"`
	ExpiringCount int64            `json:"expiring_count"`
	TodayRevenue  int64            `json:"today_revenue_kip"`
	Sales         []SalesPoint     `json:"sales"`
	LowStock      []LowStock       `json:"low_stock"`
	LowStockCount int64            `json:"low_stock_count"`
	RecentOrders  []domain.Order   `json:"recent_orders"`
}

func (s *Service) Dashboard(ctx context.Context, a domain.Actor) (Dashboard, error) {
	result := Dashboard{StatusCounts: map[string]int64{}, LowStock: []LowStock{}, Sales: []SalesPoint{}}
	if err := Require(a, "dashboard.view"); err != nil {
		return result, err
	}
	for _, status := range []string{"PENDING_PAYMENT", "CONFIRMED", "DELIVERED", "CANCELLED", "EXPIRED"} {
		n, err := s.Store.Count(ctx, Orders, Query{Eq: map[string]any{"order_status": status}})
		if err != nil {
			return result, err
		}
		result.StatusCounts[status] = n
	}
	var err error
	result.ExpiringCount, err = s.Store.Count(ctx, Orders, Query{Eq: map[string]any{"order_status": "PENDING_PAYMENT"}, LT: map[string]any{"reservation_expires_at": time.Now().Add(24 * time.Hour)}})
	if err != nil {
		return result, err
	}
	if err := s.Store.Find(ctx, Orders, Query{Sort: "created_at", Desc: true, Limit: 8}, &result.RecentOrders); err != nil {
		return result, err
	}
	zone, err := time.LoadLocation("Asia/Vientiane")
	if err != nil {
		return result, err
	}
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	start := today.AddDate(0, 0, -13)
	byDay := map[string]SalesPoint{}
	offset := 0
	for {
		var orders []domain.Order
		if err := s.Store.Find(ctx, Orders, Query{Eq: map[string]any{"payment_status": "PAID"}, GT: map[string]any{"created_at": start.Add(-time.Nanosecond)}, Sort: "created_at", Limit: 200, Offset: offset}, &orders); err != nil {
			return result, err
		}
		for _, order := range orders {
			key := order.CreatedAt.In(zone).Format("2006-01-02")
			point := byDay[key]
			point.Date = key
			point.Revenue, err = domain.AddMoney(point.Revenue, order.TotalKip)
			if err != nil {
				return result, err
			}
			var items []domain.Item
			if err := s.Store.Find(ctx, Items, Query{Eq: map[string]any{"order_id": order.BillNumber}}, &items); err != nil {
				return result, err
			}
			for _, item := range items {
				point.Items += item.Quantity
			}
			byDay[key] = point
		}
		if len(orders) < 200 {
			break
		}
		offset += 200
	}
	for i := 0; i < 14; i++ {
		key := start.AddDate(0, 0, i).Format("2006-01-02")
		point := byDay[key]
		point.Date = key
		result.Sales = append(result.Sales, point)
	}
	result.TodayRevenue = byDay[today.Format("2006-01-02")].Revenue
	offset = 0
	for {
		var variants []domain.Variant
		if err := s.Store.Find(ctx, Variants, Query{Sort: "id", Limit: 200, Offset: offset}, &variants); err != nil {
			return result, err
		}
		for _, v := range variants {
			if v.Available() > 5 {
				continue
			}
			p, err := findOne[domain.Product](ctx, s.Store, Products, byID(v.ProductID))
			if err != nil {
				return result, err
			}
			if p.DeletedAt != nil || p.Status != "ACTIVE" {
				continue
			}
			result.LowStockCount++
			if len(result.LowStock) < 6 {
				result.LowStock = append(result.LowStock, LowStock{Variant: v, ProductName: p.Name, ProductSlug: p.Slug})
			}
		}
		if len(variants) < 200 {
			break
		}
		offset += 200
	}
	return result, nil
}
