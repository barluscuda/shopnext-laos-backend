package integration

import (
	"bytes"
	"github.com/chai2010/webp"
	"image"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"testing"
)

func ownerAuthorization(t *testing.T, h *harness) *http.Cookie {
	t.Helper()
	_, err := h.s.CreateStaff(ctx, domain.Actor{}, application.StaffInput{Email: owner.Email, Name: "Owner", Role: "OWNER", Password: "correct-password"}, true, "ip")
	must(t, err)
	w := request(t, h, "POST", "/api/v1/admin/auth/login", map[string]string{"email": owner.Email, "password": "correct-password"})
	expect(t, w, 200)
	return &http.Cookie{Name: "Authorization", Value: "Bearer " + data[adminLogin](t, w).AccessToken}
}
func TestManagementCRUDAndReorder(t *testing.T) {
	h := setup(t)
	staff := ownerAuthorization(t, h)
	w := request(t, h, "POST", "/api/v1/admin/categories", application.CategoryInput{Name: "Home", NameEn: "Home", Slug: "home"}, staff)
	expect(t, w, 200)
	cat := data[domain.Category](t, w)
	expect(t, request(t, h, "PUT", "/api/v1/admin/categories/"+cat.ID, application.CategoryInput{Name: "New Home", NameEn: "Home", Slug: "home"}, staff), 200)
	expect(t, request(t, h, "POST", "/api/v1/admin/categories/reorder", application.ReorderInput{IDs: []string{cat.ID}}, staff), 200)
	w = request(t, h, "POST", "/api/v1/admin/products", application.ProductInput{CategoryID: cat.ID, Name: "Product", NameEn: "Product", Slug: "product", BasePriceKip: 50000, Status: "ACTIVE", Variants: []application.VariantInput{{SKU: "A1", StockOnHand: 5}, {SKU: "A2", StockOnHand: 5}}}, staff)
	expect(t, w, 200)
	p := data[domain.Product](t, w)
	expect(t, request(t, h, "PUT", "/api/v1/admin/products/"+p.ID, application.ProductInput{CategoryID: cat.ID, Name: "Updated", NameEn: "Updated", SKU: p.SKU, Slug: p.Slug, BasePriceKip: 60000, Status: "ACTIVE"}, staff), 200)
	w = request(t, h, "POST", "/api/v1/admin/products/"+p.ID+"/variants", application.VariantInput{SKU: "A3", StockOnHand: 3}, staff)
	expect(t, w, 200)
	v := data[domain.Variant](t, w)
	expect(t, request(t, h, "PUT", "/api/v1/admin/products/"+p.ID+"/variants/"+v.ID, application.VariantInput{SKU: "A3", StockOnHand: 4}, staff), 200)
	variants := rows[domain.Variant](t, h, application.Variants, application.Query{Eq: map[string]any{"product_id": p.ID}, Sort: "position"})
	ids := []string{variants[2].ID, variants[1].ID, variants[0].ID}
	expect(t, request(t, h, "POST", "/api/v1/admin/products/"+p.ID+"/variants/reorder", application.ReorderInput{IDs: ids}, staff), 200)
	expect(t, request(t, h, "DELETE", "/api/v1/admin/products/"+p.ID+"/variants/"+v.ID, nil, staff), 200)
	expect(t, request(t, h, "DELETE", "/api/v1/admin/products/"+p.ID, nil, staff), 200)
	expect(t, request(t, h, "GET", "/api/v1/products/product", nil), 404)
	expect(t, request(t, h, "POST", "/api/v1/admin/products/"+p.ID+"/restore", nil, staff), 200)
	w = request(t, h, "POST", "/api/v1/admin/providers", map[string]any{"name": "Express", "shipping_fee_kip": 20000}, staff)
	expect(t, w, 200)
	provider := data[domain.Provider](t, w)
	expect(t, request(t, h, "PUT", "/api/v1/admin/providers/"+provider.ID, map[string]any{"name": "Express Updated", "shipping_fee_kip": 25000}, staff), 200)
	geo := application.Geography()[0]
	w = request(t, h, "POST", "/api/v1/admin/branches", map[string]string{"provider_id": provider.ID, "province": geo.Name, "city": geo.Districts[0], "name": "Branch"}, staff)
	expect(t, w, 200)
	branch := data[domain.Branch](t, w)
	expect(t, request(t, h, "DELETE", "/api/v1/admin/branches/"+branch.ID, nil, staff), 200)
	// Exercise multipart upload through Gin, then attach the resulting media.
	var img, body bytes.Buffer
	must(t, webp.Encode(&img, image.NewRGBA(image.Rect(0, 0, 600, 200)), &webp.Options{Lossless: true}))
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="image.webp"`)
	header.Set("Content-Type", "image/webp")
	part, err := form.CreatePart(header)
	must(t, err)
	_, err = part.Write(img.Bytes())
	must(t, err)
	must(t, form.Close())
	req := httptest.NewRequest("POST", "/api/v1/admin/uploads?kind=content", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-ShopNext-CSRF", "1")
	req.Header.Set(staff.Name, staff.Value)
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	expect(t, w, 200)
	mediaURL := data[map[string]string](t, w)["url"]
	expect(t, request(t, h, "GET", mediaURL, nil), 200)
	w = request(t, h, "POST", "/api/v1/admin/products/"+p.ID+"/images", application.ImageInput{URL: mediaURL, AltText: "Product"}, staff)
	expect(t, w, 200)
	attachment := data[domain.Image](t, w)
	expect(t, request(t, h, "POST", "/api/v1/admin/products/"+p.ID+"/images/reorder", application.ReorderInput{IDs: []string{attachment.ID}}, staff), 200)
	for _, resource := range []string{"hero-slides", "promo-banners"} {
		payload := map[string]any{"image_url": mediaURL, "heading": "Sale", "cta_href": "/products", "active": true}
		w = request(t, h, "POST", "/api/v1/admin/"+resource, payload, staff)
		expect(t, w, 200)
		c := data[domain.Content](t, w)
		payload["active"] = false
		expect(t, request(t, h, "PUT", "/api/v1/admin/"+resource+"/"+c.ID, payload, staff), 200)
		expect(t, request(t, h, "POST", "/api/v1/admin/"+resource+"/reorder", application.ReorderInput{IDs: []string{c.ID}}, staff), 200)
		expect(t, request(t, h, "DELETE", "/api/v1/admin/"+resource+"/"+c.ID, nil, staff), 200)
	}
	expect(t, request(t, h, "DELETE", "/api/v1/admin/products/"+p.ID+"/images/"+attachment.ID, nil, staff), 200)
	expect(t, request(t, h, "GET", mediaURL, nil), 404)
	expect(t, request(t, h, "GET", "/api/v1/admin/orders?sort=id", nil, staff), 400)
	expect(t, request(t, h, "DELETE", "/api/v1/admin/categories/"+cat.ID, nil, staff), 409)
}
