package application

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestTrustedDeviceJWTValidation(t *testing.T) {
	service := &Service{Options: Options{TrustDeviceSecret: strings.Repeat("s", 32)}}
	now := time.Now().Unix()
	claims := deviceClaims{"B1234567", "random-token", now, now + 3600}
	token, err := service.signDevice(claims)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := service.deviceClaims(token, "B1234567")
	if err != nil || parsed != claims {
		t.Fatal("valid JWT rejected")
	}
	parts := strings.Split(token, ".")
	invalid := []string{"", token + "x", parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"order_id":"OTHER"}`)) + "." + parts[2], base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + "." + parts[2]}
	for _, key := range invalid {
		if _, err := service.deviceClaims(key, "B1234567"); err == nil {
			t.Fatal("invalid JWT accepted")
		}
	}
	if _, err := service.deviceClaims(token, "OTHER"); err == nil {
		t.Fatal("cross-order JWT accepted")
	}
	expired, err := service.signDevice(deviceClaims{"B1234567", "random", now - 3600, now - 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.deviceClaims(expired, "B1234567"); err == nil {
		t.Fatal("expired JWT accepted")
	}
	other := &Service{Options: Options{TrustDeviceSecret: strings.Repeat("x", 32)}}
	if _, err := other.deviceClaims(token, "B1234567"); err == nil {
		t.Fatal("wrong signing secret accepted")
	}
}
