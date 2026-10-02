package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"hash/fnv"
	"reflect"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"strings"
	"time"
)

type Store struct {
	DB *gorm.DB
	tx bool
}

// entityAllowed is explicit so new resources cannot silently become accessible.
func entityAllowed(e application.Entity) bool {
	switch e {
	case application.Categories, application.Products, application.Variants, application.Images, application.HeroSlides, application.PromoBanners, application.Providers, application.Branches, application.Orders, application.Items, application.OrderEvents, application.Attempts, application.PaymentEvents, application.Refunds, application.OTPs, application.PhoneTokens, application.TrustedDevices, application.ClientNotifications, application.StaffUsers, application.Sessions, application.Audits, application.OutboxEvents, application.SMSLogs, application.Idempotencies:
		return true
	}
	return false
}
func Open(url string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: false})
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(20)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	return &Store{DB: db}, nil
}
func (s *Store) Ping(ctx context.Context) error {
	db, err := s.DB.DB()
	if err != nil {
		return err
	}
	return db.PingContext(ctx)
}
func (s *Store) Close() error {
	db, err := s.DB.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
func (s *Store) Transaction(ctx context.Context, fn func(application.Store) error) error {
	return translate(s.DB.WithContext(ctx).Transaction(func(db *gorm.DB) error { return fn(&Store{DB: db, tx: true}) }))
}
func (s *Store) LockKey(ctx context.Context, key string) error {
	if !s.tx {
		return errors.New("advisory lock requires transaction")
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return s.DB.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(?)", int64(h.Sum64())).Error
}
func safeField(field string) bool {
	if field == "" {
		return false
	}
	for _, c := range field {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
func (s *Store) query(ctx context.Context, e application.Entity, q application.Query) (*gorm.DB, error) {
	if !entityAllowed(e) {
		return nil, errors.New("unknown entity")
	}
	db := s.DB.WithContext(ctx).Table(string(e))
	for field, value := range q.Eq {
		if !safeField(field) {
			return nil, errors.New("invalid field")
		}
		if value == nil {
			db = db.Where(clause.Eq{Column: field, Value: nil})
		} else {
			db = db.Where(clause.Eq{Column: field, Value: value})
		}
	}
	for f, v := range q.In {
		if !safeField(f) {
			return nil, errors.New("invalid field")
		}
		db = db.Where(clause.IN{Column: f, Values: toAny(v)})
	}
	for f, v := range q.LT {
		if !safeField(f) {
			return nil, errors.New("invalid field")
		}
		db = db.Where(clause.Lt{Column: f, Value: v})
	}
	for f, v := range q.GT {
		if !safeField(f) {
			return nil, errors.New("invalid field")
		}
		db = db.Where(clause.Gt{Column: f, Value: v})
	}
	if q.Search != "" {
		fields := searchFields(e)
		if len(fields) > 0 {
			parts := []string{}
			args := []any{}
			term := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q.Search) + "%"
			for _, f := range fields {
				parts = append(parts, f+" ILIKE ?")
				args = append(args, term)
			}
			db = db.Where("("+strings.Join(parts, " OR ")+")", args...)
		}
	}
	if q.Sort != "" {
		if !safeField(q.Sort) {
			return nil, errors.New("invalid sort")
		}
		db = db.Order(clause.OrderByColumn{Column: clause.Column{Name: q.Sort}, Desc: q.Desc})
		if e == application.Orders && q.Sort != "bill_number" {
			db = db.Order("bill_number")
		} else if e == application.Variants && q.Sort == "position" {
			db = db.Order("sku")
		} else if q.Sort != "id" && e != application.Orders {
			db = db.Order("id")
		}
	}
	if q.Lock {
		if !s.tx {
			return nil, errors.New("row lock requires transaction")
		}
		l := clause.Locking{Strength: "UPDATE"}
		if q.SkipLocked {
			l.Options = "SKIP LOCKED"
		}
		db = db.Clauses(l)
	}
	return db, nil
}
func searchFields(e application.Entity) []string {
	switch e {
	case application.Products:
		return []string{"name", "name_en", "sku", "slug"}
	case application.Orders:
		return []string{"bill_number", "recipient_phone", "recipient_name"}
	case application.Attempts:
		return []string{"order_id", "provider_transaction_id"}
	case application.PaymentEvents:
		return []string{"bill_number", "provider_transaction_id"}
	case application.SMSLogs:
		return []string{"phone", "message"}
	case application.StaffUsers:
		return []string{"email", "name"}
	case application.Categories:
		return []string{"name", "name_en"}
	case application.Branches:
		return []string{"name", "province", "city"}
	case application.Providers:
		return []string{"name"}
	case application.Audits:
		return []string{"actor_email", "action"}
	case application.Refunds:
		return []string{"order_id", "reason"}
	}
	return nil
}
func toAny(v []string) []any {
	r := make([]any, len(v))
	for i, x := range v {
		r[i] = x
	}
	return r
}
func (s *Store) Find(ctx context.Context, e application.Entity, q application.Query, out any) error {
	db, err := s.query(ctx, e, q)
	if err != nil {
		return err
	}
	if q.Limit > 0 {
		db = db.Limit(q.Limit)
	}
	if q.Offset > 0 {
		db = db.Offset(q.Offset)
	}
	return translate(db.Find(out).Error)
}
func (s *Store) Count(ctx context.Context, e application.Entity, q application.Query) (int64, error) {
	q.Sort = ""
	q.Lock = false
	db, err := s.query(ctx, e, q)
	if err != nil {
		return 0, err
	}
	var n int64
	err = db.Count(&n).Error
	return n, translate(err)
}

// Mapping belongs to this adapter. Domain records contain no ORM tags.
func persistenceFields(record any) map[string]any {
	result := map[string]any{}
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			val := v.Field(i)
			if f.Anonymous {
				walk(val)
				continue
			}
			name := schema.NamingStrategy{}.ColumnName("", f.Name)
			if val.Type() == reflect.TypeFor[json.RawMessage]() {
				result[name] = string(val.Bytes())
			} else {
				result[name] = val.Interface()
			}
		}
	}
	walk(reflect.ValueOf(record))
	return result
}
func (s *Store) Insert(ctx context.Context, e application.Entity, record any) error {
	if !entityAllowed(e) {
		return errors.New("unknown entity")
	}
	return translate(s.DB.WithContext(ctx).Table(string(e)).Create(persistenceFields(record)).Error)
}
func (s *Store) Update(ctx context.Context, e application.Entity, id string, values map[string]any) error {
	if !entityAllowed(e) {
		return errors.New("unknown entity")
	}
	pk := "id"
	if e == application.Orders {
		pk = "bill_number"
	}
	for f := range values {
		if !safeField(f) {
			return errors.New("invalid field")
		}
	}
	r := s.DB.WithContext(ctx).Table(string(e)).Where(clause.Eq{Column: pk, Value: id}).Updates(values)
	if r.Error != nil {
		return translate(r.Error)
	}
	if r.RowsAffected != 1 {
		return domain.ErrNotFound
	}
	return nil
}
func (s *Store) Delete(ctx context.Context, e application.Entity, id string) error {
	if !entityAllowed(e) {
		return errors.New("unknown entity")
	}
	pk := "id"
	if e == application.Orders {
		pk = "bill_number"
	}
	r := s.DB.WithContext(ctx).Exec(fmt.Sprintf("DELETE FROM %s WHERE %s = ?", e, pk), id)
	if r.Error != nil {
		return translate(r.Error)
	}
	if r.RowsAffected != 1 {
		return domain.ErrNotFound
	}
	return nil
}
func translate(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return domain.Fail("ALREADY_EXISTS", 409)
		case "23503":
			return domain.Fail("REFERENCED_RESOURCE", 409)
		case "23514", "23502", "22P02", "22003":
			return domain.Fail("INVALID_DATA", 400)
		}
	}
	return err
}

var _ application.Store = (*Store)(nil)

func (s *Store) Catalog(ctx context.Context, category, search, sortField string, offset, limit int) ([]domain.Product, int64, error) {
	base := s.DB.WithContext(ctx).Table("products p").Joins("JOIN categories c ON c.id=p.category_id").Where("p.status='ACTIVE' AND p.deleted_at IS NULL").Where("EXISTS (SELECT 1 FROM variants WHERE product_id=p.id)")
	if category != "" {
		base = base.Where("c.slug=? AND c.parent_id IS NULL", category)
	}
	if search != "" {
		term := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search) + "%"
		base = base.Where("p.name LIKE ? OR p.name_en ILIKE ?", term, term)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	query := base.Session(&gorm.Session{}).Joins("JOIN LATERAL (SELECT COALESCE(v.price_override_kip,p.base_price_kip) AS price FROM variants v WHERE v.product_id=p.id ORDER BY (v.stock_on_hand-v.stock_reserved>0) DESC,v.position,v.sku LIMIT 1) display ON true")
	switch sortField {
	case "price-asc":
		query = query.Order("display.price ASC")
	case "price-desc":
		query = query.Order("display.price DESC")
	default:
		query = query.Order("p.created_at DESC")
	}
	var products []domain.Product
	err := query.Select("p.*").Order("p.id").Offset(offset).Limit(limit).Scan(&products).Error
	return products, total, err
}
