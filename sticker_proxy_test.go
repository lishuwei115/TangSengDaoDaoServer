package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStickerSearchProxyMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotPath string
	var gotQuery string
	var gotPrefix string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotPrefix = r.Header.Get("X-Forwarded-Prefix")
		w.Header().Set("X-Sticker-Upstream", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()

	r := gin.New()
	r.Use(stickerSearchProxyMiddleware(upstream.URL))
	r.GET("/normal", func(c *gin.Context) {
		c.String(http.StatusOK, "normal")
	})

	req := httptest.NewRequest(http.MethodGet, "/sticker-api/v1/search?q=%E5%BC%80%E5%BF%83&limit=20", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if gotPath != "/v1/search" {
		t.Fatalf("upstream path = %q, want %q", gotPath, "/v1/search")
	}
	if gotQuery != "q=%E5%BC%80%E5%BF%83&limit=20" {
		t.Fatalf("upstream query = %q", gotQuery)
	}
	if gotPrefix != stickerSearchPrefix {
		t.Fatalf("X-Forwarded-Prefix = %q, want %q", gotPrefix, stickerSearchPrefix)
	}
	if rec.Header().Get("X-Sticker-Upstream") != "ok" {
		t.Fatalf("upstream response header was not preserved")
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "proxied" {
		t.Fatalf("body = %q, want %q", string(body), "proxied")
	}

	normalReq := httptest.NewRequest(http.MethodGet, "/normal", nil)
	normalRec := httptest.NewRecorder()
	r.ServeHTTP(normalRec, normalReq)
	if normalRec.Code != http.StatusOK || normalRec.Body.String() != "normal" {
		t.Fatalf("non-sticker request was affected: status=%d body=%q", normalRec.Code, normalRec.Body.String())
	}
}

func TestStickerSearchProxyMiddlewareDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(stickerSearchProxyMiddleware(""))
	r.GET("/sticker-api/v1/search", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/sticker-api/v1/search", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("disabled proxy changed existing routing: status=%d", rec.Code)
	}
}

func TestStickerSearchProxyMiddlewareRejectsInvalidUpstream(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected invalid upstream to panic")
		}
	}()
	_ = stickerSearchProxyMiddleware("StickerSearch:3001")
}
