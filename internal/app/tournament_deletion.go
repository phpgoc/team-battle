package app

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *App) deleteTournament(c *gin.Context)  { a.setTournamentDeleted(c, true) }
func (a *App) restoreTournament(c *gin.Context) { a.setTournamentDeleted(c, false) }

// 删除仅改变可见性标记，恢复时沿用原来的阶段、名单和对局。
func (a *App) setTournamentDeleted(c *gin.Context, deleted bool) {
	actor, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if actor.Role != roleSuperAdmin {
		fail(c, http.StatusForbidden, "只有超级管理员可以删除或恢复赛事")
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "赛事编号无效")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	defer tx.Rollback()
	var previous bool
	var status string
	if err := tx.QueryRow(`SELECT is_deleted,status FROM tournaments WHERE id=?`, id).Scan(&previous, &status); err != nil {
		if err == sql.ErrNoRows {
			fail(c, http.StatusNotFound, "赛事不存在")
		} else {
			fail(c, http.StatusInternalServerError, "读取赛事失败")
		}
		return
	}
	if status != statusCompleted && status != statusCancelled {
		fail(c, http.StatusBadRequest, "仅已关闭赛事可以删除或恢复")
		return
	}
	var operations []tournamentOperation
	// 重复请求不追加审计，也不改动原赛事数据。
	if previous != deleted {
		if _, err := tx.Exec(`UPDATE tournaments SET is_deleted=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, deleted, id); err != nil {
			fail(c, http.StatusInternalServerError, "保存失败")
			return
		}
		action := "tournament_restored"
		if deleted {
			action = "tournament_deleted"
		}
		engine := &matchEngine{db: tx, tournamentID: id, actorID: actor.ID}
		if err := engine.log(action, gin.H{"is_deleted": deleted}); err != nil {
			fail(c, http.StatusInternalServerError, "保存失败")
			return
		}
		operations = engine.operations
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	for _, operation := range operations {
		a.logTournamentOperation(operation)
	}
	a.invalidateHistoryCache()
	c.Header("Cache-Control", "no-store")
	ok(c, gin.H{"id": id, "is_deleted": deleted})
}
