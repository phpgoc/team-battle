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
	r.Use(a.rejectDuringShutdown)
	r.Use(a.broadcastAfterMutation)
	a.registerPageRoutes(r)
	r.GET("/live/:id/setting", func(c *gin.Context) { a.serveLiveEntry(c, true) })
	r.GET("/live/:id/lineup", a.serveLineupEntry)
	r.GET("/live/:id", func(c *gin.Context) { a.serveLiveEntry(c, false) })
	r.GET("/assets/styles.css", a.serveStyles)
	r.GET("/assets/app.js", a.serveAppScript)
	for _, asset := range []struct{ path, contentType string }{{"kof-rules.js", "application/javascript"}, {"match-draw.js", "application/javascript"}, {"match-draw.css", "text/css"}, {"lineup-page.js", "application/javascript"}, {"lineup-page.css", "text/css"}, {"kof-board.js", "application/javascript"}, {"kof-board.css", "text/css"}, {"archive-page.js", "application/javascript"}, {"archive-page.css", "text/css"}, {"confirm-dialog.js", "application/javascript"}, {"confirm-dialog.css", "text/css"}, {"result-correction.css", "text/css"}, {"broadcast-layout.css", "text/css"}} {
		path, contentType := asset.path, asset.contentType
		r.GET("/assets/"+path, func(c *gin.Context) {
			data, err := webFS.ReadFile("web/" + path)
			if err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Data(http.StatusOK, contentType+"; charset=utf-8", data)
		})
	}
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

	for _, asset := range []struct{ path, contentType string }{{"tournament-management.js", "application/javascript"}, {"tournament-management.css", "text/css"}, {"search-select.js", "application/javascript"}, {"search-select.css", "text/css"}, {"date-picker.js", "application/javascript"}, {"date-picker.css", "text/css"}, {"site-settings.css", "text/css"}, {"management.css", "text/css"}, {"vendor/flatpickr/flatpickr.min.js", "application/javascript"}, {"vendor/flatpickr/flatpickr.min.css", "text/css"}, {"vendor/flatpickr/zh.js", "application/javascript"}, {"vendor/flatpickr/ko.js", "application/javascript"}} {
		path, contentType := asset.path, asset.contentType
		r.GET("/assets/"+path, func(c *gin.Context) {
			data, err := webFS.ReadFile("web/" + path)
			if err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Data(http.StatusOK, contentType+"; charset=utf-8", data)
		})
	}

	// 登录读取请求和校验密码时无需占用赛事写锁。
	r.POST("/api/auth/login", a.login)
	api := r.Group("/api")
	api.Use(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			a.writeMu.Lock()
			defer a.writeMu.Unlock()
		}
		c.Next()
	})
	api.POST("/auth/logout", a.logout)
	api.GET("/auth/me", a.me)
	api.PATCH("/auth/profile", a.updateProfile)
	api.POST("/auth/avatar", a.uploadAvatar)
	api.GET("/settings/catalog", a.settingsCatalog)
	api.GET("/backgrounds", a.listBackgrounds)
	api.GET("/backgrounds/:id/image", a.backgroundImage)
	api.GET("/backgrounds/:id/thumbnail", a.backgroundThumbnail)
	api.GET("/categories/:id/icon", a.categoryIcon)
	api.GET("/extra-infos/:id/icon", a.extraInfoIcon)
	api.GET("/users/:id/avatar", a.avatar)
	api.GET("/public/tournaments", a.publicTournaments)
	api.GET("/public/history", a.publicHistory)
	api.GET("/public/users/:id/records", a.publicPlayerRecords)
	api.GET("/public/tournaments/:id", a.publicTournament)
	api.GET("/public/tournaments/:id/stream", limitSSEConnections(a.sseMaxConnections), a.publicStream)

	api.GET("/users", a.listUsers)
	api.POST("/users", a.createUser)
	api.PATCH("/users/:id", a.updateUser)
	api.POST("/users/:id/avatar", a.uploadUserAvatar)
	api.DELETE("/users/:id", a.deleteUser)
	api.PATCH("/users/:id/password", a.resetUserPassword)
	api.GET("/tournaments", a.listTournaments)
	api.POST("/tournaments", a.createTournament)
	api.POST("/tournaments/archive", a.archiveTournament)
	api.DELETE("/tournaments/:id", a.deleteTournament)
	api.POST("/tournaments/:id/restore", a.restoreTournament)
	api.POST("/tournaments/:id/players", a.addTournamentPlayers)
	api.GET("/tournaments/:id/eligible-users", a.eligibleTournamentUsers)
	api.GET("/site-settings", a.siteSettings)
	api.PATCH("/site-settings/tournament-defaults", a.updateTournamentDefaults)
	api.POST("/site-settings/backgrounds", a.createBackground)
	api.PATCH("/site-settings/backgrounds/:backgroundID", a.updateBackground)
	api.DELETE("/site-settings/backgrounds/:backgroundID", a.deleteBackground)
	api.POST("/site-settings/tournament-names", a.createTournamentName)
	api.PATCH("/site-settings/tournament-names/:nameID", a.updateTournamentName)
	api.DELETE("/site-settings/tournament-names/:nameID", a.deleteTournamentName)
	api.POST("/site-settings/categories", a.createCategory)
	api.PATCH("/site-settings/categories/:categoryID", a.updateCategory)
	api.DELETE("/site-settings/categories/:categoryID", a.deleteCategory)
	api.POST("/site-settings/extra-infos", a.createExtraInfo)
	api.PATCH("/site-settings/extra-infos/:extraInfoID", a.updateExtraInfo)
	api.DELETE("/site-settings/extra-infos/:extraInfoID", a.deleteExtraInfo)
	api.PATCH("/tournaments/:id/settings", a.updateTournamentSettings)
	api.POST("/tournaments/:id/setup-draw", a.drawTournamentSetup)
	api.PATCH("/tournaments/:id/draw-settings", a.updateDrawSettings)
	api.PATCH("/tournaments/:id/background", a.updateTournamentBackground)
	api.GET("/tournaments/:id", a.tournamentDetail)
	api.POST("/tournaments/:id/captain-votes", a.castCaptainVote)
	api.POST("/tournaments/:id/captains/confirm", a.confirmCaptains)
	api.POST("/tournaments/:id/captains/select", a.selectTeamCaptain)
	api.POST("/tournaments/:id/lineup/generate", a.generateLineup)
	api.POST("/tournaments/:id/lineup/submit", a.submitLineup)
	api.GET("/tournaments/:id/lineup", a.lineupOptions)
	api.POST("/tournaments/:id/start", a.startTournament)
	api.POST("/tournaments/:id/opening-result", a.submitOpeningResult)
	api.POST("/tournaments/:id/matches/:matchID/result", a.submitResult)
	api.PATCH("/tournaments/:id/matches/:matchID/result", a.correctResult)
	api.POST("/tournaments/:id/kof/next-player", a.selectKOFPlayer)
	api.GET("/tournaments/:id/kof/options", a.kofOptions)
	api.POST("/tournaments/:id/kof/starter", a.selectKOFStarter)
	api.POST("/tournaments/:id/ace/confirm", a.confirmAce)
	api.POST("/tournaments/:id/ace/draw", a.drawAceManually)
	api.POST("/tournaments/:id/cancel", a.cancelTournament)
	api.POST("/tournaments/:id/confirm", a.confirmTournament)
	return r
}

// 没有完成参赛分队时，赛事只允许有权限的账号进入设置页。
func (a *App) serveLiveEntry(c *gin.Context, setting bool) {
	tournamentID, err := parseID(c.Param("id"))
	if err != nil || !a.tournamentExists(tournamentID) {
		c.Status(http.StatusNotFound)
		return
	}
	user, loggedIn := a.currentUser(c)
	manager := loggedIn == nil && a.canManageTournament(user.ID, user.Role, tournamentID)
	ready, err := loadTournamentReadiness(a.db, tournamentID)
	if err != nil {
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
	if !ready.publicReady {
		if manager {
			c.Redirect(http.StatusFound, "/live/"+strconv.FormatInt(tournamentID, 10)+"/setting")
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
