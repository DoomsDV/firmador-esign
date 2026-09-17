package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SignHeaders firma el body crudo con HMAC-SHA256 estilo Stripe: t=<unix>,v1=<hex>.
func SignHeaders(secret []byte, body []byte, now time.Time) (timestamp string, signature string) {
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	return ts, fmt.Sprintf("t=%s,v1=%s", ts, sig)
}

// VerifySignature valida firma entrante (para tests). Tolerancia en segundos.
func VerifySignature(secret []byte, body []byte, timestamp string, signatureHeader string, tolerance time.Duration) bool {
	ts, sig, ok := parseSignatureHeader(signatureHeader)
	if !ok {
		return false
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	if tolerance > 0 {
		if tsInt < now-int64(tolerance.Seconds()) || tsInt > now+int64(tolerance.Seconds()) {
			return false
		}
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func parseSignatureHeader(header string) (timestamp, sig string, ok bool) {
	parts := strings.Split(header, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "t=") {
			timestamp = strings.TrimPrefix(p, "t=")
		}
		if strings.HasPrefix(p, "v1=") {
			sig = strings.TrimPrefix(p, "v1=")
		}
	}
	ok = timestamp != "" && sig != ""
	return timestamp, sig, ok
}
