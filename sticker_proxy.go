package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const stickerSearchPrefix = "/sticker-api"

// stickerSearchProxyMiddleware proxies /sticker-api/* requests to the configured
// StickerSearch service. When no upstream is configured, it is a no-op and the
// existing TangSengDaoDaoServer routing behavior is unchanged.
func stickerSearchProxyMiddleware(rawUpstream string) gin.HandlerFunc {
	rawUpstream = strings.TrimSpace(rawUpstream)
	if rawUpstream == "" {
		return func(c *gin.Context) {}
	}

	target, err := url.Parse(rawUpstream)
	if err != nil || target.Scheme == "" || target.Host == "" {
		panic(fmt.Sprintf("invalid StickerSearch upstream %q", rawUpstream))
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		stripStickerSearchPrefix(req.URL)
		originalDirector(req)
		req.Host = target.Host
		req.Header.Set("X-Forwarded-Prefix", stickerSearchPrefix)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"sticker search unavailable"}`))
	}

	return func(c *gin.Context) {
		if !isStickerSearchPath(c.Request.URL.Path) {
			return
		}

		proxy.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}
}

func isStickerSearchPath(path string) bool {
	return path == stickerSearchPrefix || strings.HasPrefix(path, stickerSearchPrefix+"/")
}

func stripStickerSearchPrefix(u *url.URL) {
	u.Path = strings.TrimPrefix(u.Path, stickerSearchPrefix)
	if u.Path == "" {
		u.Path = "/"
	}

	if u.RawPath != "" {
		u.RawPath = strings.TrimPrefix(u.RawPath, stickerSearchPrefix)
		if u.RawPath == "" {
			u.RawPath = "/"
		}
	}
}
