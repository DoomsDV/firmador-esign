package webhook

import (
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	secret := []byte("whsec_test_secret_key_32bytes!!")
	body := []byte(`{"event":"invoice.ready","cdc":"0123456789012345678901234567890123456789012"}`)
	now := time.Now()
	ts, sig := SignHeaders(secret, body, now)
	if !VerifySignature(secret, body, ts, sig, 5*time.Minute) {
		t.Fatal("expected valid signature")
	}
	if VerifySignature(secret, body, ts, "t=1,v1=deadbeef", 5*time.Minute) {
		t.Fatal("expected invalid signature")
	}
}

func TestValidateWebhookURL(t *testing.T) {
	if err := ValidateWebhookURL("https://example.com/hook"); err != nil {
		t.Fatalf("valid url: %v", err)
	}
	if err := ValidateWebhookURL("http://example.com/hook"); err == nil {
		t.Fatal("expected http to fail")
	}
	if err := ValidateWebhookURL("https://127.0.0.1/hook"); err == nil {
		t.Fatal("expected loopback to fail")
	}
}
