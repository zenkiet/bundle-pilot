package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckOriginBehindProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "http://172.18.0.5:8080/__gateway/config", nil)
	r.Header.Set("Origin", "http://10.8.0.6:8000")
	if checkOrigin(r) == nil {
		t.Fatal("a write from another host passed")
	}
	r.Header.Set("X-Forwarded-Host", "10.8.0.6:8000")
	if err := checkOrigin(r); err != nil {
		t.Fatalf("a same-origin write through the proxy was refused: %v", err)
	}
	r.Header.Set("Origin", "http://evil.example")
	if checkOrigin(r) == nil {
		t.Fatal("a cross-site write through the proxy passed")
	}
}
