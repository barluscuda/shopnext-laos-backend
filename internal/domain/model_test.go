package domain

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestMoney(t *testing.T) {
	for _, test := range []struct {
		a, b  int64
		valid bool
	}{{50000, 2, true}, {math.MaxInt64, 1, true}, {math.MaxInt64, 2, false}, {-1, 1, false}, {0, 99, true}, {1, 100, false}, {1, 0, false}} {
		_, err := LineTotal(test.a, test.b)
		if (err == nil) != test.valid {
			t.Fatalf("line %d*%d: %v", test.a, test.b, err)
		}
	}
	if _, err := AddMoney(math.MaxInt64, 1); err == nil {
		t.Fatal("addition overflow")
	}
	if _, err := AddMoney(-1, 2); err == nil {
		t.Fatal("negative money")
	}
}
func TestPhonesAndMasking(t *testing.T) {
	for _, p := range []string{"+856 20 5555-5555", "8562055555555", "02055555555"} {
		if n := NormalizePhone(p); n != "02055555555" || !ValidPhone(n) {
			t.Fatal(n)
		}
	}
	for _, p := range []string{"2055555555", "0201", "0205555555555555", "invalid"} {
		if ValidPhone(p) {
			t.Fatal(p)
		}
	}
	if MaskName("Alice Customer") != "A___e C______r" {
		t.Fatal(MaskName("Alice Customer"))
	}
	if MaskName("ສົມພອນ") == "ສົມພອນ" {
		t.Fatal("Unicode masking")
	}
}
func TestIdentifiersAndPermissions(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		b := BillNumber()
		if len(b) != 8 || b[0] != 'B' || strings.ContainsAny(b, "01ILO") || seen[b] {
			t.Fatal(b)
		}
		seen[b] = true
		if len(Digits()) != 6 {
			t.Fatal("pickup code")
		}
	}
	if !Can("FINANCE", "refunds.approve") || Can("SUPPORT", "refunds.approve") || Can("AUDITOR", "orders.act") || Can("UNKNOWN", "orders.view") {
		t.Fatal("permissions")
	}
}
func TestSensitiveJSON(t *testing.T) {
	for _, v := range []any{Staff{PasswordHash: "SECRET"}, PhoneToken{TokenHash: "SECRET"}, OTP{CodeHash: "SECRET", ChallengeHash: "SECRET"}, Session{TokenHash: "SECRET"}, Outbox{Message: "SECRET"}} {
		raw, err := json.Marshal(v)
		if err != nil || strings.Contains(string(raw), "SECRET") {
			t.Fatal("secret serialized")
		}
	}
}
