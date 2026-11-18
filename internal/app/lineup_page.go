package app

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 排阵编辑单独鉴权：队长仅可读取本队，管理员可以代排双方。
func (a *App) lineupOptions(c *gin.Context) {
	actor, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil || !a.tournamentExists(id) {
		fail(c, http.StatusNotFound, "赛事不存在")
		return
	}
	manager := a.canManageTournament(actor.ID, actor.Role, id)
	var captain int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=? AND active=1 AND is_captain=1`, id, actor.ID).Scan(&captain); err != nil {
		fail(c, 500, "读取排阵失败")
		return
	}
	if !manager && captain == 0 {
		fail(c, http.StatusForbidden, "只有本队队长或赛事管理员可以排阵")
		return
	}
	rows, err := a.db.Query(`SELECT team_id,position,player_id,source FROM lineup_entries WHERE tournament_id=? AND (? OR team_id IN (SELECT team_id FROM tournament_players WHERE tournament_id=? AND user_id=? AND active=1 AND is_captain=1)) ORDER BY team_id,position`, id, manager, id, actor.ID)
	if err != nil {
		fail(c, 500, "读取排阵失败")
		return
	}
	defer rows.Close()
	lineups := []map[string]any{}
	for rows.Next() {
		var team, player int64
		var position int
		var source string
		if err := rows.Scan(&team, &position, &player, &source); err != nil {
			fail(c, 500, "读取排阵失败")
			return
		}
		lineups = append(lineups, gin.H{"team_id": team, "position": position, "player_id": player, "source": source})
	}
	if err := rows.Err(); err != nil {
		fail(c, 500, "读取排阵失败")
		return
	}
	c.Header("Cache-Control", "no-store")
	ok(c, gin.H{"lineups": lineups})
}

// 独立排阵页仅对未开赛的手动排阵赛事开放，管理员可切换双方队伍。
func (a *App) serveLineupEntry(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil || !a.tournamentExists(id) {
		c.Status(http.StatusNotFound)
		return
	}
	actor, err := a.currentUser(c)
	if err != nil {
		c.Status(http.StatusForbidden)
		return
	}
	manager := a.canManageTournament(actor.ID, actor.Role, id)
	var captain int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM tournament_players WHERE tournament_id=? AND user_id=? AND active=1 AND is_captain=1`, id, actor.ID).Scan(&captain); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if !manager && captain == 0 {
		c.Status(http.StatusForbidden)
		return
	}
	var stage, raw string
	if err := a.db.QueryRow(`SELECT status,rules_json FROM tournaments WHERE id=?`, id).Scan(&stage, &raw); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	rules, err := decodeRules(raw)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	var teams int
	if err := a.db.QueryRow(`SELECT COUNT(DISTINCT team_id) FROM tournament_players WHERE tournament_id=? AND active=1`, id).Scan(&teams); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if !preMatch(stage) || rules.Round1Mode != "manual" || teams != 2 {
		c.Redirect(http.StatusFound, "/live/"+strconv.FormatInt(id, 10))
		return
	}
	a.serveIndex(c)
}
