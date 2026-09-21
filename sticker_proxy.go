package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/config"
	"github.com/TangSengDaoDao/TangSengDaoDaoServerLib/pkg/wkhttp"
	"github.com/gin-gonic/gin"
)

const stickerSearchPrefix = "/sticker-api"

type stickerSearchAuthenticator func(c *gin.Context) bool

// newStickerSearchAuthenticator reuses TangSengDaoDao's existing login token
// cache. StickerSearch therefore has no separate account/authentication system.
func newStickerSearchAuthenticator(ctx *config.Context) stickerSearchAuthenticator {
	return func(c *gin.Context) bool {
		token := strings.TrimSpace(c.GetHeader("token"))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"msg": "token不能为空，请先登录！",
			})
			return false
		}

		tokenInfo, err := wkhttp.GetLoginTokenInfo(
			token,
			ctx.GetConfig().Cache.TokenCachePrefix,
			ctx.Cache(),
		)
		if err != nil || tokenInfo == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"msg": "请先登录！",
			})
			return false
		}

		c.Set("uid", tokenInfo.UID)
		c.Set("name", tokenInfo.Name)
		if tokenInfo.Role != "" {
			c.Set("role", tokenInfo.Role)
		}
		return true
	}
}

// stickerSearchProxyMiddleware proxies /sticker-api/* requests to the configured
// StickerSearch service. Health endpoints remain public; every other
// StickerSearch endpoint must pass TangSengDaoDao login authentication.
func stickerSearchProxyMiddleware(rawUpstream string, authenticate stickerSearchAuthenticator) gin.HandlerFunc {
	rawUpstream = strings.TrimSpace(rawUpstream)
	if rawUpstream == "" {
		return func(c *gin.Context) {}
	}
	if authenticate == nil {
		panic("StickerSearch authenticator is required")
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
		// The application login token is only for the TangSengDaoDao gateway.
		// Do not leak it to the internal StickerSearch service.
		req.Header.Del("token")
		req.Header.Set("X-Forwarded-Prefix", stickerSearchPrefix)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"sticker search unavailable"}`))
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !isStickerSearchPath(path) {
			return
		}

		if !isStickerSearchPublicPath(path) && !authenticate(c) {
			return
		}

		proxy.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}
}

func isStickerSearchPath(path string) bool {
	return path == stickerSearchPrefix || strings.HasPrefix(path, stickerSearchPrefix+"/")
}

func isStickerSearchPublicPath(path string) bool {
	switch path {
	case stickerSearchPrefix + "/health",
		stickerSearchPrefix + "/healthz",
		stickerSearchPrefix + "/v1/health",
		stickerSearchPrefix + "/v1/readyz":
		return true
	default:
		return false
	}
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
