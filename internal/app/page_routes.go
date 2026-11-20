package app

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 页面使用独立路径；查询参数仅用于筛选、分页和登录后的返回地址。
func (a *App) registerPageRoutes(r *gin.Engine) {
	r.GET("/", a.serveIndex)
	r.GET("/login", a.serveIndex)
	r.GET("/account", func(c *gin.Context) { a.serveWorkspaceEntry(c, false) })
	for _, path := range []string{
		"/tournaments", "/tournaments/new", "/tournaments/archive", "/accounts", "/accounts/new", "/backgrounds",
		"/site-settings", "/site-settings/tournament-names", "/site-settings/categories", "/site-settings/extra-infos",
	} {
		r.GET(path, func(c *gin.Context) { a.serveWorkspaceEntry(c, true) })
	}
	r.GET("/accounts/:id", func(c *gin.Context) {
		if !a.authorizeWorkspaceEntry(c, true) {
			return
		}
		id, err := parseID(c.Param("id"))
		if err != nil || id <= 0 {
			c.Status(http.StatusNotFound)
			return
		}
		if _, err := a.userByID(id); err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		a.serveIndex(c)
	})
	// 旧书签只负责跳转，不再使用查询参数渲染页面。
	r.GET("/workspace", a.redirectLegacyWorkspace)
	for _, path := range []string{"/history", "/live"} {
		r.GET(path, func(c *gin.Context) {
			target := "/"
			if query := c.Request.URL.RawQuery; query != "" {
				target += "?" + query
			}
			c.Redirect(http.StatusMovedPermanently, target)
		})
	}
}

func (a *App) authorizeWorkspaceEntry(c *gin.Context, admin bool) bool {
	user, err := a.currentUser(c)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?return="+url.QueryEscape(c.Request.URL.RequestURI()))
		return false
	}
	if admin && user.Role != roleSuperAdmin {
		c.Status(http.StatusForbidden)
		return false
	}
	c.Header("Cache-Control", "no-store")
	return true
}

func (a *App) serveWorkspaceEntry(c *gin.Context, admin bool) {
	if a.authorizeWorkspaceEntry(c, admin) {
		a.serveIndex(c)
	}
}

func (a *App) redirectLegacyWorkspace(c *gin.Context) {
	paths := map[string]string{
		"": "/", "home": "/", "settings": "/account",
		"createEvent": "/tournaments/new", "events": "/tournaments/new",
		"accounts": "/accounts", "users": "/accounts", "passwords": "/accounts", "createUser": "/accounts/new",
		"archive": "/tournaments/archive", "siteSettings": "/site-settings", "site-settings": "/site-settings", "backgrounds": "/backgrounds",
	}
	target, exists := paths[c.Query("page")]
	if !exists {
		c.Status(http.StatusNotFound)
		return
	}
	if target == "/accounts" {
		if edit := c.Query("edit"); edit == "new" {
			target += "/new"
		} else if edit != "" {
			id, err := parseID(edit)
			if err != nil || id <= 0 {
				c.Status(http.StatusNotFound)
				return
			}
			target += "/" + strconv.FormatInt(id, 10)
		}
	}
	if target == "/" {
		query := c.Request.URL.Query()
		query.Del("page")
		query.Del("edit")
		if encoded := query.Encode(); encoded != "" {
			target += "?" + encoded
		}
	}
	c.Redirect(http.StatusMovedPermanently, target)
}
