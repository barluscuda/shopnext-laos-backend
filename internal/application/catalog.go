package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"shopnext-laos/internal/domain"
	"strings"
	"time"
	"unicode/utf8"
)

type ProductView struct {
	domain.Product
	Variants         []domain.Variant `json:"variants"`
	Images           []domain.Image   `json:"images"`
	Category         domain.Category  `json:"category"`
	InStock          int64            `json:"in_stock"`
	DisplayVariantID string           `json:"display_variant_id"`
	DisplayPriceKip  int64            `json:"display_price_kip"`
	IsNew            bool             `json:"is_new"`
}

func (s *Service) productView(ctx context.Context, p domain.Product) (ProductView, error) {
	r := ProductView{Product: p, IsNew: p.CreatedAt.Add(14 * 24 * time.Hour).After(time.Now())}
	var err error
	if r.Category, err = findOne[domain.Category](ctx, s.Store, Categories, byID(p.CategoryID)); err != nil {
		return r, err
	}
	if err = s.Store.Find(ctx, Variants, Query{Eq: map[string]any{"product_id": p.ID}, Sort: "position"}, &r.Variants); err != nil {
		return r, err
	}
	if err = s.Store.Find(ctx, Images, Query{Eq: map[string]any{"product_id": p.ID}, Sort: "position"}, &r.Images); err != nil {
		return r, err
	}
	for _, v := range r.Variants {
		r.InStock, err = domain.AddMoney(r.InStock, v.Available())
		if err != nil {
			return r, err
		}
		if r.DisplayVariantID == "" && v.Available() > 0 {
			r.DisplayVariantID = v.ID
			r.DisplayPriceKip = p.BasePriceKip
			if v.PriceOverrideKip != nil {
				r.DisplayPriceKip = *v.PriceOverrideKip
			}
		}
	}
	if r.DisplayVariantID == "" && len(r.Variants) > 0 {
		v := r.Variants[0]
		r.DisplayVariantID = v.ID
		r.DisplayPriceKip = p.BasePriceKip
		if v.PriceOverrideKip != nil {
			r.DisplayPriceKip = *v.PriceOverrideKip
		}
	}
	return r, nil
}
func (s *Service) Catalog(ctx context.Context, category, search, sortField string, page, limit int) ([]ProductView, int64, error) {
	products, total, err := s.Store.Catalog(ctx, category, strings.TrimSpace(search), sortField, (page-1)*limit, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]ProductView, 0, len(products))
	for _, p := range products {
		view, err := s.productView(ctx, p)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, view)
	}
	return result, total, nil
}
func (s *Service) Product(ctx context.Context, slug string, admin bool) (ProductView, error) {
	q := Query{Eq: map[string]any{"slug": slug}}
	if !admin {
		q.Eq["status"] = "ACTIVE"
		q.Eq["deleted_at"] = nil
	}
	p, err := findOne[domain.Product](ctx, s.Store, Products, q)
	if err != nil {
		return ProductView{}, err
	}
	return s.productView(ctx, p)
}
func (s *Service) Related(ctx context.Context, slug string) ([]ProductView, error) {
	p, err := s.Product(ctx, slug, false)
	if err != nil {
		return nil, err
	}
	cards, _, err := s.Catalog(ctx, p.Category.Slug, "", "newest", 1, 5)
	if err != nil {
		return nil, err
	}
	result := make([]ProductView, 0, 4)
	for _, card := range cards {
		if card.ID != p.ID && len(result) < 4 {
			result = append(result, card)
		}
	}
	return result, nil
}

type DeliveryProvider struct {
	domain.Provider
	Branches []domain.Branch `json:"branches"`
}

func (s *Service) Delivery(ctx context.Context) ([]DeliveryProvider, error) {
	var providers []domain.Provider
	if err := s.Store.Find(ctx, Providers, Query{Sort: "name"}, &providers); err != nil {
		return nil, err
	}
	result := make([]DeliveryProvider, 0, len(providers))
	for _, p := range providers {
		r := DeliveryProvider{Provider: p}
		if err := s.Store.Find(ctx, Branches, Query{Eq: map[string]any{"provider_id": p.ID}, Sort: "province"}, &r.Branches); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

type VariantInput struct {
	SKU              string            `json:"sku"`
	Attributes       map[string]string `json:"attributes"`
	PriceOverrideKip *int64            `json:"price_override_kip"`
	StockOnHand      int64             `json:"stock_on_hand"`
	Position         int               `json:"position"`
}
type ProductInput struct {
	CategoryID   string         `json:"category_id"`
	SKU          string         `json:"sku"`
	Name         string         `json:"name"`
	NameEn       string         `json:"name_en"`
	Slug         string         `json:"slug"`
	Description  string         `json:"description"`
	BasePriceKip int64          `json:"base_price_kip"`
	Status       string         `json:"status"`
	InitialStock int64          `json:"initial_stock"`
	Variants     []VariantInput `json:"variants"`
	Images       []ImageInput   `json:"images"`
}
type ImageInput struct {
	URL      string `json:"url"`
	AltText  string `json:"alt_text"`
	Position int    `json:"position"`
}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validName(v string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(v))
	return n >= 2 && n <= 160
}
func validSKU(v string) bool { return len(v) >= 2 && len(v) <= 40 }
func validateVariant(v VariantInput) error {
	if !validSKU(v.SKU) || v.StockOnHand < 0 || v.StockOnHand > 1_000_000_000 || v.Position < 0 || v.PriceOverrideKip != nil && (*v.PriceOverrideKip < 0 || *v.PriceOverrideKip > 2_000_000_000) || len(v.Attributes) > 30 {
		return domain.Fail("INVALID_VARIANT", 400)
	}
	for k, val := range v.Attributes {
		if strings.TrimSpace(k) == "" || len(k) > 120 || len(val) > 240 {
			return domain.Fail("INVALID_ATTRIBUTES", 400)
		}
	}
	return nil
}
func (s *Service) SaveProduct(ctx context.Context, a domain.Actor, id string, in ProductInput, ip string) (domain.Product, error) {
	var product domain.Product
	if err := Require(a, "products.manage"); err != nil {
		return product, err
	}
	in.Name = strings.TrimSpace(in.Name)
	in.NameEn = strings.TrimSpace(in.NameEn)
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Status == "" {
		in.Status = "DRAFT"
	}
	if in.SKU == "" {
		in.SKU = "P-" + strings.ToUpper(in.Slug)
	}
	if !validName(in.Name) || !validName(in.NameEn) || utf8.RuneCountInString(in.Name) > 120 || utf8.RuneCountInString(in.NameEn) > 120 || !slugRE.MatchString(in.Slug) || len(in.Slug) > 160 || !validSKU(in.SKU) || in.BasePriceKip < 0 || in.BasePriceKip > 2_000_000_000 || utf8.RuneCountInString(in.Description) > 2000 || in.InitialStock < 0 || in.InitialStock > 1_000_000_000 || in.Status != "DRAFT" && in.Status != "ACTIVE" && in.Status != "ARCHIVED" || len(in.Variants) > 50 || len(in.Images) > 30 {
		return product, domain.Fail("INVALID_PRODUCT", 400)
	}
	if id == "" && len(in.Variants) == 0 {
		in.Variants = []VariantInput{{SKU: in.SKU + "-D", Attributes: map[string]string{"ຕົວເລືອກ": "ມາດຕະຖານ"}, StockOnHand: in.InitialStock}}
	}
	for i := range in.Variants {
		if len(in.Variants) == 1 && in.Variants[i].SKU == "" {
			in.Variants[i].SKU = in.SKU + "-D"
		}
		if err := validateVariant(in.Variants[i]); err != nil {
			return product, err
		}
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Category](ctx, tx, Categories, byID(in.CategoryID)); err != nil {
			return err
		}
		if id != "" {
			var err error
			product, err = findOne[domain.Product](ctx, tx, Products, locked(byID(id)))
			if err != nil {
				return err
			}
			if len(in.Variants) > 0 || len(in.Images) > 0 {
				return domain.Fail("USE_VARIANT_OR_IMAGE_ENDPOINT", 400)
			}
		} else {
			product.Base = domain.NewBase()
		}
		product.CategoryID = in.CategoryID
		product.SKU = in.SKU
		product.Name = in.Name
		product.NameEn = in.NameEn
		product.Slug = in.Slug
		product.Description = in.Description
		product.BasePriceKip = in.BasePriceKip
		product.Status = in.Status
		if id == "" {
			if err := tx.Insert(ctx, Products, &product); err != nil {
				return err
			}
			for i, v := range in.Variants {
				attrs, _ := json.Marshal(v.Attributes)
				if v.Attributes == nil {
					attrs = []byte("{}")
				}
				if err := tx.Insert(ctx, Variants, &domain.Variant{Base: domain.NewBase(), ProductID: product.ID, SKU: v.SKU, Attributes: attrs, PriceOverrideKip: v.PriceOverrideKip, StockOnHand: v.StockOnHand, Position: i}); err != nil {
					return err
				}
			}
			for i, img := range in.Images {
				if !s.Media.ValidateURL(img.URL) {
					return domain.Fail("INVALID_MEDIA_URL", 400)
				}
				if err := tx.Insert(ctx, Images, &domain.Image{Base: domain.NewBase(), ProductID: product.ID, URL: img.URL, AltText: img.AltText, Position: i}); err != nil {
					return err
				}
			}
		} else {
			if err := tx.Update(ctx, Products, id, changed(map[string]any{"category_id": product.CategoryID, "sku": product.SKU, "name": product.Name, "name_en": product.NameEn, "slug": product.Slug, "description": product.Description, "base_price_kip": product.BasePriceKip, "status": product.Status})); err != nil {
				return err
			}
		}
		return s.Audit(ctx, tx, a, "product.save", "product", product.ID, "", ip)
	})
	return product, err
}
func (s *Service) ProductVisibility(ctx context.Context, a domain.Actor, id string, restore bool, ip string) error {
	if err := Require(a, "products.manage"); err != nil {
		return err
	}
	return s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Product](ctx, tx, Products, locked(byID(id))); err != nil {
			return err
		}
		var deleted any = time.Now().UTC()
		action := "product.delete"
		if restore {
			deleted = nil
			action = "product.restore"
		}
		if err := tx.Update(ctx, Products, id, changed(map[string]any{"deleted_at": deleted})); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, action, "product", id, "", ip)
	})
}
func (s *Service) SaveVariant(ctx context.Context, a domain.Actor, productID, id string, in VariantInput, ip string) (domain.Variant, error) {
	var variant domain.Variant
	if err := Require(a, "products.manage"); err != nil {
		return variant, err
	}
	if err := validateVariant(in); err != nil {
		return variant, err
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Product](ctx, tx, Products, locked(byID(productID))); err != nil {
			return err
		}
		if id != "" {
			var err error
			variant, err = findOne[domain.Variant](ctx, tx, Variants, locked(byID(id)))
			if err != nil {
				return err
			}
			if variant.ProductID != productID {
				return domain.Fail("FORBIDDEN", 403)
			}
		} else {
			variant.Base = domain.NewBase()
			variant.ProductID = productID
		}
		if in.StockOnHand < variant.StockReserved {
			return domain.Fail("STOCK_BELOW_RESERVED", 409)
		}
		attrs, _ := json.Marshal(in.Attributes)
		if in.Attributes == nil {
			attrs = []byte("{}")
		}
		variant.SKU = in.SKU
		variant.Attributes = attrs
		variant.PriceOverrideKip = in.PriceOverrideKip
		variant.StockOnHand = in.StockOnHand
		variant.Position = in.Position
		if id == "" {
			if err := tx.Insert(ctx, Variants, &variant); err != nil {
				return err
			}
		} else {
			if err := tx.Update(ctx, Variants, id, changed(map[string]any{"sku": variant.SKU, "attributes": string(attrs), "price_override_kip": variant.PriceOverrideKip, "stock_on_hand": variant.StockOnHand, "position": variant.Position})); err != nil {
				return err
			}
		}
		return s.Audit(ctx, tx, a, "variant.save", "variant", variant.ID, "", ip)
	})
	return variant, err
}
func (s *Service) DeleteVariant(ctx context.Context, a domain.Actor, productID, id, ip string) error {
	if err := Require(a, "products.manage"); err != nil {
		return err
	}
	return s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Product](ctx, tx, Products, locked(byID(productID))); err != nil {
			return err
		}
		variant, err := findOne[domain.Variant](ctx, tx, Variants, locked(byID(id)))
		if err != nil {
			return err
		}
		if variant.ProductID != productID {
			return domain.Fail("FORBIDDEN", 403)
		}
		count, err := tx.Count(ctx, Variants, Query{Eq: map[string]any{"product_id": productID}})
		if err != nil {
			return err
		}
		if count <= 1 {
			return domain.Fail("LAST_VARIANT", 409)
		}
		if err := tx.Delete(ctx, Variants, id); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "variant.delete", "variant", id, "", ip)
	})
}
func (s *Service) SaveImage(ctx context.Context, a domain.Actor, productID string, in ImageInput, ip string) (domain.Image, error) {
	image := domain.Image{Base: domain.NewBase(), ProductID: productID, URL: in.URL, AltText: in.AltText, Position: in.Position}
	if err := Require(a, "products.manage"); err != nil {
		return image, err
	}
	if !s.Media.ValidateURL(in.URL) || len(in.AltText) > 500 || in.Position < 0 {
		return image, domain.Fail("INVALID_IMAGE", 400)
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Product](ctx, tx, Products, locked(byID(productID))); err != nil {
			return err
		}
		if err := tx.Insert(ctx, Images, &image); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "image.create", "image", image.ID, "", ip)
	})
	return image, err
}
func (s *Service) DeleteImage(ctx context.Context, a domain.Actor, productID, id, ip string) error {
	if err := Require(a, "products.manage"); err != nil {
		return err
	}
	var mediaURL string
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Product](ctx, tx, Products, locked(byID(productID))); err != nil {
			return err
		}
		image, err := findOne[domain.Image](ctx, tx, Images, locked(byID(id)))
		if err != nil {
			return err
		}
		if image.ProductID != productID {
			return domain.Fail("FORBIDDEN", 403)
		}
		mediaURL = image.URL
		if err := tx.Delete(ctx, Images, id); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "image.delete", "image", id, "", ip)
	})
	if err != nil {
		return err
	}
	return s.deleteUnusedMedia(ctx, mediaURL)
}
func (s *Service) deleteUnusedMedia(ctx context.Context, url string) error {
	for _, resource := range []Entity{Images, HeroSlides, PromoBanners} {
		field := "image_url"
		if resource == Images {
			field = "url"
		}
		n, err := s.Store.Count(ctx, resource, Query{Eq: map[string]any{field: url}})
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
	return s.Media.Delete(ctx, url)
}

type CategoryInput struct {
	Name     string  `json:"name"`
	NameEn   string  `json:"name_en"`
	Slug     string  `json:"slug"`
	ParentID *string `json:"parent_id"`
}

func (s *Service) SaveCategory(ctx context.Context, a domain.Actor, id string, in CategoryInput, ip string) (domain.Category, error) {
	var category domain.Category
	in.Name, in.NameEn, in.Slug = strings.TrimSpace(in.Name), strings.TrimSpace(in.NameEn), strings.TrimSpace(in.Slug)
	if err := Require(a, "taxonomy.manage"); err != nil {
		return category, err
	}
	if !validName(in.Name) || !validName(in.NameEn) || utf8.RuneCountInString(in.Name) > 60 || utf8.RuneCountInString(in.NameEn) > 60 || !slugRE.MatchString(in.Slug) || len(in.Slug) > 160 {
		return category, domain.Fail("INVALID_CATEGORY", 400)
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if err := tx.LockKey(ctx, "categories:tree"); err != nil {
			return err
		}
		if id == "" {
			category.Base = domain.NewBase()
		} else {
			var err error
			category, err = findOne[domain.Category](ctx, tx, Categories, locked(byID(id)))
			if err != nil {
				return err
			}
		}
		cursor := in.ParentID
		visited := map[string]bool{}
		for cursor != nil {
			if *cursor == category.ID || visited[*cursor] {
				return domain.Fail("CATEGORY_CYCLE", 409)
			}
			visited[*cursor] = true
			parent, err := findOne[domain.Category](ctx, tx, Categories, byID(*cursor))
			if err != nil {
				return err
			}
			cursor = parent.ParentID
		}
		if id == "" || (category.ParentID == nil) != (in.ParentID == nil) || category.ParentID != nil && in.ParentID != nil && *category.ParentID != *in.ParentID {
			var siblings []domain.Category
			if err := tx.Find(ctx, Categories, Query{Eq: map[string]any{"parent_id": in.ParentID}, Sort: "sort_order", Desc: true, Limit: 1}, &siblings); err != nil {
				return err
			}
			category.SortOrder = 0
			if len(siblings) > 0 {
				category.SortOrder = siblings[0].SortOrder + 1
			}
		}
		category.Name = in.Name
		category.NameEn = in.NameEn
		category.Slug = in.Slug
		category.ParentID = in.ParentID
		if id == "" {
			if err := tx.Insert(ctx, Categories, &category); err != nil {
				return err
			}
		} else {
			if err := tx.Update(ctx, Categories, id, changed(map[string]any{"name": in.Name, "name_en": in.NameEn, "slug": in.Slug, "parent_id": in.ParentID, "sort_order": category.SortOrder})); err != nil {
				return err
			}
		}
		return s.Audit(ctx, tx, a, "category.save", "category", category.ID, "", ip)
	})
	return category, err
}
func (s *Service) SaveProvider(ctx context.Context, a domain.Actor, id string, in domain.Provider, ip string) (domain.Provider, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := Require(a, "taxonomy.manage"); err != nil {
		return in, err
	}
	if !validName(in.Name) || utf8.RuneCountInString(in.Name) > 60 || in.ShippingFeeKip < 0 || in.ShippingFeeKip > 1_000_000 {
		return in, domain.Fail("INVALID_PROVIDER", 400)
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if id == "" {
			if err := tx.LockKey(ctx, "provider-name:"+in.Name); err != nil {
				return err
			}
			n, err := tx.Count(ctx, Providers, Query{Eq: map[string]any{"name": in.Name}})
			if err != nil {
				return err
			}
			if n > 0 {
				return domain.Fail("ALREADY_EXISTS", 409)
			}
			in.Base = domain.NewBase()
			if err := tx.Insert(ctx, Providers, &in); err != nil {
				return err
			}
		} else {
			current, err := findOne[domain.Provider](ctx, tx, Providers, locked(byID(id)))
			if err != nil {
				return err
			}
			in.Base = current.Base
			in.UpdatedAt = time.Now().UTC()
			if err := tx.Update(ctx, Providers, id, changed(map[string]any{"name": in.Name, "shipping_fee_kip": in.ShippingFeeKip})); err != nil {
				return err
			}
		}
		return s.Audit(ctx, tx, a, "provider.save", "provider", in.ID, "", ip)
	})
	return in, err
}
func (s *Service) SaveBranch(ctx context.Context, a domain.Actor, in domain.Branch, ip string) (domain.Branch, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := Require(a, "taxonomy.manage"); err != nil {
		return in, err
	}
	if !validName(in.Name) || utf8.RuneCountInString(in.Name) > 80 || !ValidDistrict(in.Province, in.City) {
		return in, domain.Fail("INVALID_BRANCH", 400)
	}
	in.Base = domain.NewBase()
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if err := tx.Insert(ctx, Branches, &in); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "branch.create", "branch", in.ID, "", ip)
	})
	return in, err
}
func validLink(link string) bool {
	if link == "" {
		return true
	}
	if strings.HasPrefix(link, "/") && !strings.HasPrefix(link, "//") {
		return true
	}
	u, err := url.Parse(link)
	return err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http")
}
func (s *Service) SaveContent(ctx context.Context, a domain.Actor, resource Entity, id string, in domain.Content, ip string) (domain.Content, error) {
	if err := Require(a, "content.manage"); err != nil {
		return in, err
	}
	if resource != HeroSlides && resource != PromoBanners {
		return in, domain.Fail("INVALID_RESOURCE", 400)
	}
	if !s.Media.ValidateContentURL(in.ImageURL) || !validLink(in.CTAHref) || len(in.Heading) > 500 || len(in.Subheading) > 1000 || len(in.CTALabel) > 160 || len(in.AltText) > 500 || in.Position < 0 {
		return in, domain.Fail("INVALID_CONTENT", 400)
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if id == "" {
			in.Base = domain.NewBase()
			if err := tx.Insert(ctx, resource, &in); err != nil {
				return err
			}
		} else {
			if _, err := findOne[domain.Content](ctx, tx, resource, locked(byID(id))); err != nil {
				return err
			}
			in.ID = id
			if err := tx.Update(ctx, resource, id, changed(map[string]any{"image_url": in.ImageURL, "alt_text": in.AltText, "heading": in.Heading, "subheading": in.Subheading, "cta_label": in.CTALabel, "cta_href": in.CTAHref, "position": in.Position, "active": in.Active})); err != nil {
				return err
			}
		}
		return s.Audit(ctx, tx, a, "content.save", string(resource), in.ID, "", ip)
	})
	return in, err
}
func (s *Service) DeleteResource(ctx context.Context, a domain.Actor, resource Entity, id, ip string) error {
	permission := "taxonomy.manage"
	if resource == HeroSlides || resource == PromoBanners {
		permission = "content.manage"
	} else if resource != Categories && resource != Branches {
		return domain.Fail("INVALID_RESOURCE", 400)
	}
	if err := Require(a, permission); err != nil {
		return err
	}
	var mediaURL string
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if resource == Categories {
			if err := tx.LockKey(ctx, "categories:tree"); err != nil {
				return err
			}
		}
		if resource == HeroSlides || resource == PromoBanners {
			content, err := findOne[domain.Content](ctx, tx, resource, locked(byID(id)))
			if err != nil {
				return err
			}
			mediaURL = content.ImageURL
		}
		if err := tx.Delete(ctx, resource, id); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "resource.delete", string(resource), id, "", ip)
	})
	if err != nil {
		return err
	}
	if mediaURL != "" {
		return s.deleteUnusedMedia(ctx, mediaURL)
	}
	return nil
}

type ReorderInput struct {
	IDs      []string `json:"ids"`
	ParentID *string  `json:"parent_id"`
}

func (s *Service) Reorder(ctx context.Context, a domain.Actor, resource Entity, productID string, in ReorderInput, ip string) error {
	permission := "products.manage"
	switch resource {
	case Categories:
		permission = "taxonomy.manage"
	case HeroSlides, PromoBanners:
		permission = "content.manage"
	case Variants, Images:
	default:
		return domain.Fail("INVALID_RESOURCE", 400)
	}
	if err := Require(a, permission); err != nil {
		return err
	}
	if len(in.IDs) == 0 || len(in.IDs) > 500 {
		return domain.Fail("INVALID_ORDERING", 400)
	}
	return s.Store.Transaction(ctx, func(tx Store) error {
		if resource == Categories {
			if err := tx.LockKey(ctx, "categories:tree"); err != nil {
				return err
			}
		} else if resource == Variants || resource == Images {
			if _, err := findOne[domain.Product](ctx, tx, Products, locked(byID(productID))); err != nil {
				return err
			}
		} else {
			if err := tx.LockKey(ctx, "reorder:"+string(resource)); err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		for index, id := range in.IDs {
			if seen[id] {
				return domain.Fail("INVALID_ORDERING", 400)
			}
			seen[id] = true
			q := byID(id)
			if resource == Variants || resource == Images {
				q.Eq["product_id"] = productID
			}
			if resource == Categories {
				q.Eq["parent_id"] = in.ParentID
			}
			n, err := tx.Count(ctx, resource, q)
			if err != nil {
				return err
			}
			if n != 1 {
				return domain.Fail("INVALID_ORDERING_GROUP", 400)
			}
			column := "position"
			if resource == Categories {
				column = "sort_order"
			}
			if err := tx.Update(ctx, resource, id, changed(map[string]any{column: index})); err != nil {
				return err
			}
		}
		return s.Audit(ctx, tx, a, "resource.reorder", string(resource), productID, "", ip)
	})
}
func IsNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }
