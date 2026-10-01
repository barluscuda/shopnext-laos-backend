package application

import (
	"encoding/base64"
	"testing"
)

func TestPasswordHashAndCompatibility(t *testing.T) {
	hash, err := HashPassword("long-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("long-password-123", hash) || VerifyPassword("wrong", hash) || !VerifyPassword("long-password-123", base64.StdEncoding.EncodeToString([]byte(hash))) {
		t.Fatal("scrypt compatibility")
	}
	for _, bad := range []string{"", "scrypt$wrong$wrong", "notbase64", "scrypt$00000000000000000000000000000000$zz"} {
		if VerifyPassword("password", bad) {
			t.Fatal(bad)
		}
	}
}
func TestGeography(t *testing.T) {
	g := Geography()
	if len(g) != 18 {
		t.Fatal(len(g))
	}
	for _, p := range g {
		for _, d := range p.Districts {
			if !ValidDistrict(p.Name, d) {
				t.Fatal(p, d)
			}
		}
	}
	if ValidDistrict(g[0].Name, "invalid") || ValidDistrict("invalid", g[0].Districts[0]) {
		t.Fatal("invalid geography accepted")
	}
}
