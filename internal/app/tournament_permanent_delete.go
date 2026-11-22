package app

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 进行中的赛事允许其管理员永久删除，所有关联业务表通过外键级联清理。
func (a *App) permanentlyDeleteTournament(c *gin.Context) {
	_, tournamentID, access := a.managerTournament(c)
	if !access {
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除赛事失败")
		return
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRow(`SELECT status FROM tournaments WHERE id=?`, tournamentID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			fail(c, http.StatusNotFound, "赛事不存在")
		} else {
			fail(c, http.StatusInternalServerError, "读取赛事失败")
		}
		return
	}
	if status == statusCompleted || status == statusCancelled {
		fail(c, http.StatusBadRequest, "已结束或已取消的赛事不能永久删除。")
		return
	}
	before, err := tournamentRecordContributions(tx, tournamentID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取选手战绩失败")
		return
	}
	result, err := tx.Exec(`DELETE FROM tournaments WHERE id=?`, tournamentID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除赛事失败")
		return
	}
	deleted, err := result.RowsAffected()
	if err != nil || deleted != 1 {
		fail(c, http.StatusConflict, "赛事状态已变化，请刷新后重试")
		return
	}
	if err := saveTournamentRecordChanges(tx, tournamentID, before); err != nil {
		fail(c, http.StatusInternalServerError, "保存选手战绩失败")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "删除赛事失败")
		return
	}
	a.invalidateHistoryCache()
	c.Header("Cache-Control", "no-store")
	ok(c, gin.H{"id": tournamentID, "deleted": true})
}
