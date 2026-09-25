package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","build":"v1"}`))
	}))
	defer ok.Close()
	if err := checkHealth(context.Background(), ok.URL); err != nil {
		t.Errorf("healthy server: %v", err)
	}

	for name, h := range map[string]http.HandlerFunc{
		"500":        func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "x", 500) },
		"not ok":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"starting"}`)) },
		"not json":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) },
		"redirected": func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", http.StatusSeeOther) },
	} {
		srv := httptest.NewServer(h)
		if err := checkHealth(context.Background(), srv.URL); err == nil {
			t.Errorf("%s: reported healthy", name)
		}
		srv.Close()
	}
	if err := checkHealth(context.Background(), "http://127.0.0.1:1/health"); err == nil {
		t.Error("nothing listening: reported healthy")
	}
}
