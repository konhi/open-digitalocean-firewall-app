package ip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPDetector_SuccessAndFallback(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-an-ip"))
	}))
	defer invalid.Close()

	ipv6Only := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("2001:db8::1\n"))
	}))
	defer ipv6Only.Close()

	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.42\n"))
	}))
	defer working.Close()

	d := &HTTPDetector{
		Client:    working.Client(),
		Endpoints: []string{failing.URL, invalid.URL, ipv6Only.URL, working.URL},
	}

	got, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() unexpected error: %v", err)
	}
	if got != "203.0.113.42" {
		t.Fatalf("Detect() = %q, want %q", got, "203.0.113.42")
	}
}

func TestHTTPDetector_AllFail(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failing.Close()

	d := &HTTPDetector{
		Client:    failing.Client(),
		Endpoints: []string{failing.URL},
	}

	if _, err := d.Detect(context.Background()); err == nil {
		t.Fatal("Detect() expected error when all endpoints fail, got nil")
	}
}
