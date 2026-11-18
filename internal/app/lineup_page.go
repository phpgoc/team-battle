package app

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 独立排阵页仅对未开赛的手动排阵赛事开放，管理员可切换双方队伍。
func (a *App) serveLineupEntry(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil || !a.eventExists(id) {
		c.Status(http.StatusNotFound)
		return
	}
	actor, err := a.currentUser(c)
	if err != nil {
		c.Status(http.StatusForbidden)
		return
	}
	manager := a.canManageEvent(actor.ID, actor.Role, id)
	var captain int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=? AND user_id=? AND active=1 AND is_captain=1`, id, actor.ID).Scan(&captain); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if !manager && captain == 0 {
		c.Status(http.StatusForbidden)
		return
	}
	var stage, raw string
	if err := a.db.QueryRow(`SELECT status,rules_json FROM events WHERE id=?`, id).Scan(&stage, &raw); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	rules, err := decodeRules(raw)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	var teams int
	if err := a.db.QueryRow(`SELECT COUNT(DISTINCT team_id) FROM event_players WHERE event_id=? AND active=1`, id).Scan(&teams); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if !preMatch(stage) || rules.Round1Mode != "manual" || teams != 2 {
		c.Redirect(http.StatusFound, "/live/"+strconv.FormatInt(id, 10))
		return
	}
	a.serveIndex(c)
}
