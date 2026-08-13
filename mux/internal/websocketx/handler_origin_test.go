package websocketx_test

import (
	"net/http/httptest"
	"testing"

	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
)

func TestDefaultCheckOrigin_SameOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/ws", nil)
	r.Host = "example.com"

	if !websocketx.DefaultCheckOrigin(r) {
		t.Error("expected same-origin request to be allowed")
	}
}

func TestDefaultCheckOrigin_NoOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/ws", nil)

	if !websocketx.DefaultCheckOrigin(r) {
		t.Error("expected request without Origin to be allowed")
	}
}

func TestDefaultCheckOrigin_CrossOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/ws", nil)
	r.Host = "example.com"
	r.Header.Set("Origin", "http://evil.com")

	if websocketx.DefaultCheckOrigin(r) {
		t.Error("expected cross-origin request to be rejected")
	}
}

func TestDefaultCheckOrigin_CrossOriginHTTPS(t *testing.T) {
	r := httptest.NewRequest("GET", "https://example.com/ws", nil)
	r.Host = "example.com"
	r.Header.Set("Origin", "https://evil.com")

	if websocketx.DefaultCheckOrigin(r) {
		t.Error("expected cross-origin HTTPS request to be rejected")
	}
}
