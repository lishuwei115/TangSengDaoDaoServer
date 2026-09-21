package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func allowStickerToken(c *gin.Context) bool {
	if c.GetHeader("token") != "valid-token" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"msg": "请先登录！"})
		return false
	}
	c.Set("uid", "u1")
	c.Set("name", "tester")
	return true
}

func TestStickerSearchProxyMiddlewareAuthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotPath string
	var gotQuery string
	var gotPrefix string
	var leakedToken string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotPrefix = r.Header.Get("X-Forwarded-Prefix")
		leakedToken = r.Header.Get("token")
		w.Header().Set("X-Sticker-Upstream", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()

	r := gin.New()
	r.Use(stickerSearchProxyMiddleware(upstream.URL, allowStickerToken))
	r.GET("/normal", func(c *gin.Context) {
		c.String(http.StatusOK, "normal")
	})
	server := httptest.NewServer(r)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/sticker-api/v1/search?q=%E5%BC%80%E5%BF%83&limit=20", nil)
	req.Header.Set("token", "valid-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
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
	if leakedToken != "" {
		t.Fatalf("gateway token leaked to StickerSearch upstream")
	}
	if resp.Header.Get("X-Sticker-Upstream") != "ok" {
		t.Fatalf("upstream response header was not preserved")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "proxied" {
		t.Fatalf("body = %q, want %q", string(body), "proxied")
	}

	normalResp, err := http.Get(server.URL + "/normal")
	if err != nil {
		t.Fatalf("normal request failed: %v", err)
	}
	defer normalResp.Body.Close()
	normalBody, _ := io.ReadAll(normalResp.Body)
	if normalResp.StatusCode != http.StatusOK || string(normalBody) != "normal" {
		t.Fatalf("non-sticker request was affected: status=%d body=%q", normalResp.StatusCode, string(normalBody))
	}
}

func TestStickerSearchProxyRejectsAnonymousSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	r := gin.New()
	r.Use(stickerSearchProxyMiddleware(upstream.URL, allowStickerToken))
	server := httptest.NewServer(r)
	defer server.Close()

	resp, err := http.Get(server.URL + "/sticker-api/v1/search?q=test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if atomic.LoadInt32(&upstreamCalls) != 0 {
		t.Fatalf("unauthenticated request reached StickerSearch upstream")
	}
}

func TestStickerSearchHealthRemainsPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("upstream path = %q, want /healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	r := gin.New()
	r.Use(stickerSearchProxyMiddleware(upstream.URL, func(c *gin.Context) bool {
		t.Fatal("public health endpoint must not invoke authentication")
		return false
	}))
	server := httptest.NewServer(r)
	defer server.Close()

	resp, err := http.Get(server.URL + "/sticker-api/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestStickerSearchProxyMiddlewareDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(stickerSearchProxyMiddleware("", allowStickerToken))
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
	_ = stickerSearchProxyMiddleware("StickerSearch:3001", allowStickerToken)
}

func TestStickerSearchProxyMiddlewareRequiresAuthenticator(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected missing authenticator to panic")
		}
	}()
	_ = stickerSearchProxyMiddleware("http://StickerSearch:3001", nil)
}
