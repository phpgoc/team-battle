package app

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *App) router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/", a.serveIndex)
	r.GET("/live/:id/setting", func(c *gin.Context) { a.serveLiveEntry(c, true) })
	r.GET("/live/:id", func(c *gin.Context) { a.serveLiveEntry(c, false) })
	r.GET("/history", a.serveIndex)
	r.GET("/login", a.serveIndex)
	r.GET("/live", a.serveIndex)
	r.GET("/workspace", a.serveIndex)
	r.GET("/assets/styles.css", a.serveStyles)
	r.GET("/assets/app.js", a.serveAppScript)
	r.GET("/assets/roster-editor.css", func(c *gin.Context) {
		data, err := webFS.ReadFile("web/roster-editor.css")
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/css; charset=utf-8", data)
	})
	r.GET("/assets/roster-editor.js", func(c *gin.Context) {
		data, err := webFS.ReadFile("web/roster-editor.js")
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
	})
	r.GET("/assets/setup-draw.js", func(c *gin.Context) {
		data, err := webFS.ReadFile("web/setup-draw.js")
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
	})
	r.GET("/assets/player-records.js", func(c *gin.Context) {
		data, err := webFS.ReadFile("web/player-records.js")
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
	})
	r.GET("/assets/i18n.js", func(c *gin.Context) {
		data, err := webFS.ReadFile("web/i18n.js")
		if err != nil {
			c.Status(500)
			return
		}
		c.Data(200, "application/javascript; charset=utf-8", data)
	})

	api := r.Group("/api")
	api.Use(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			a.writeMu.Lock()
			defer a.writeMu.Unlock()
		}
		c.Next()
	})
	api.POST("/auth/login", a.login)
	api.POST("/auth/logout", a.logout)
	api.GET("/auth/me", a.me)
	api.PATCH("/auth/profile", a.updateProfile)
	api.POST("/auth/avatar", a.uploadAvatar)
	api.GET("/settings/catalog", a.settingsCatalog)
	api.GET("/backgrounds", a.listBackgrounds)
	api.GET("/backgrounds/:id/image", a.backgroundImage)
	api.GET("/categories/:id/icon", a.categoryIcon)
	api.GET("/users/:id/avatar", a.avatar)
	api.GET("/public/events", a.publicEvents)
	api.GET("/public/history", a.publicHistory)
	api.GET("/public/users/:id/records", a.publicPlayerRecords)
	api.GET("/public/events/:id", a.publicEvent)
	api.GET("/public/events/:id/stream", a.publicStream)

	api.GET("/users", a.listUsers)
	api.POST("/users", a.createUser)
	api.PATCH("/users/:id", a.updateUser)
	api.POST("/users/:id/avatar", a.uploadUserAvatar)
	api.DELETE("/users/:id", a.deleteUser)
	api.PATCH("/users/:id/password", a.resetUserPassword)
	api.GET("/events", a.listEvents)
	api.POST("/events", a.createEvent)
	api.POST("/events/archive", a.archiveEvent)
	api.POST("/events/:id/players", a.addEventPlayers)
	api.GET("/events/:id/eligible-users", a.eligibleEventUsers)
	api.GET("/site-settings", a.siteSettings)
	api.POST("/site-settings/backgrounds", a.createBackground)
	api.PATCH("/site-settings/backgrounds/:backgroundID", a.updateBackground)
	api.DELETE("/site-settings/backgrounds/:backgroundID", a.deleteBackground)
	api.POST("/site-settings/event-names", a.createEventName)
	api.PATCH("/site-settings/event-names/:nameID", a.updateEventName)
	api.DELETE("/site-settings/event-names/:nameID", a.deleteEventName)
	api.POST("/site-settings/categories", a.createCategory)
	api.PATCH("/site-settings/categories/:categoryID", a.updateCategory)
	api.DELETE("/site-settings/categories/:categoryID", a.deleteCategory)
	api.PATCH("/events/:id/settings", a.updateEventSettings)
	api.POST("/events/:id/setup-draw", a.drawEventSetup)
	api.PATCH("/events/:id/draw-settings", a.updateDrawSettings)
	api.PATCH("/events/:id/background", a.updateEventBackground)
	api.GET("/events/:id", a.eventDetail)
	api.POST("/events/:id/captain-votes", a.castCaptainVote)
	api.POST("/events/:id/captains/confirm", a.confirmCaptains)
	api.POST("/events/:id/lineup/generate", a.generateLineup)
	api.POST("/events/:id/lineup/submit", a.submitLineup)
	api.POST("/events/:id/start", a.startEvent)
	api.POST("/events/:id/matches/:matchID/result", a.submitResult)
	api.PATCH("/events/:id/matches/:matchID/result", a.correctResult)
	api.POST("/events/:id/kof/next-player", a.selectKOFPlayer)
	api.GET("/events/:id/kof/options", a.kofOptions)
	api.POST("/events/:id/kof/starter", a.selectKOFStarter)
	api.POST("/events/:id/ace/confirm", a.confirmAce)
	api.POST("/events/:id/cancel", a.cancelEvent)
	api.POST("/events/:id/confirm", a.confirmEvent)
	return r
}

// 没有完成参赛分队时，赛事只允许有权限的账号进入设置页。
func (a *App) serveLiveEntry(c *gin.Context, setting bool) {
	eventID, err := parseID(c.Param("id"))
	if err != nil || !a.eventExists(eventID) {
		c.Status(http.StatusNotFound)
		return
	}
	user, loggedIn := a.currentUser(c)
	manager := loggedIn == nil && a.canManageEvent(user.ID, user.Role, eventID)
	var assignedTeams int
	if err := a.db.QueryRow(`SELECT COUNT(DISTINCT team_id) FROM event_players WHERE event_id=? AND active=1`, eventID).Scan(&assignedTeams); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if setting {
		if !manager {
			c.Status(http.StatusForbidden)
			return
		}
		a.serveIndex(c)
		return
	}
	if assignedTeams != 2 {
		if manager {
			c.Redirect(http.StatusFound, "/live/"+strconv.FormatInt(eventID, 10)+"/setting")
			return
		}
		c.Status(http.StatusNotFound)
		return
	}
	a.serveIndex(c)
}

func (a *App) serveIndex(c *gin.Context) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}

func (a *App) serveStyles(c *gin.Context) {
	data, err := webFS.ReadFile("web/styles.css")
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Data(http.StatusOK, "text/css; charset=utf-8", data)
}

func (a *App) serveAppScript(c *gin.Context) {
	data, err := webFS.ReadFile("web/app.js")
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", data)
}
