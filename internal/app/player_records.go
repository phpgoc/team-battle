package app

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
)

type recordScore struct {
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
}

type categoryRecord struct {
	CategoryID int64  `json:"category_id"`
	Name       string `json:"name"`
	IconURL    string `json:"icon_url"`
	recordScore
}

// 战绩只提供公开资料，访客和本人共用同一份统计，不返回登录名等账号设置。
func (a *App) publicPlayerRecords(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "账号编号无效")
		return
	}
	directory, err := a.userDirectory()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取账号失败")
		return
	}
	user, exists := directory[id]
	if !exists || user.IsDeleted {
		fail(c, http.StatusNotFound, "账号不存在")
		return
	}
	// 按本人所在一侧判断胜负，先按对手聚合，再使用内存目录关联对手当前分类。
	// 尚未录入结果及已取消赛事不计入；重新查询可直接反映比分修正。
	rows, err := a.db.Query(`SELECT opponent_id,SUM(won),SUM(1-won),SUM(CASE WHEN round_no=3 THEN won ELSE 0 END),SUM(CASE WHEN round_no=3 THEN 1-won ELSE 0 END)
		FROM (
			SELECT m.player_b_id AS opponent_id,m.round_no,m.winner_team_id=m.team_a_id AS won
			FROM matches m JOIN events e ON e.id=m.event_id
			WHERE m.player_a_id=? AND m.status='finished' AND m.winner_team_id IN (m.team_a_id,m.team_b_id) AND e.status<>'cancelled'
			UNION ALL
			SELECT m.player_a_id AS opponent_id,m.round_no,m.winner_team_id=m.team_b_id AS won
			FROM matches m JOIN events e ON e.id=m.event_id
			WHERE m.player_b_id=? AND m.status='finished' AND m.winner_team_id IN (m.team_a_id,m.team_b_id) AND e.status<>'cancelled'
		) GROUP BY opponent_id`, id, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取战绩失败")
		return
	}
	defer rows.Close()
	overall, ace := recordScore{}, recordScore{}
	byCategory := map[int64]*categoryRecord{}
	for rows.Next() {
		var opponentID int64
		var score, aceScore recordScore
		if err := rows.Scan(&opponentID, &score.Wins, &score.Losses, &aceScore.Wins, &aceScore.Losses); err != nil {
			fail(c, http.StatusInternalServerError, "读取战绩失败")
			return
		}
		overall.Wins += score.Wins
		overall.Losses += score.Losses
		ace.Wins += aceScore.Wins
		ace.Losses += aceScore.Losses
		opponent := directory[opponentID]
		categoryID := opponent.CategoryID
		if opponent.CategoryName == "" {
			categoryID = 0
		}
		category := byCategory[categoryID]
		if category == nil {
			category = &categoryRecord{CategoryID: categoryID, Name: opponent.CategoryName, IconURL: opponent.CategoryIcon}
			byCategory[categoryID] = category
		}
		category.Wins += score.Wins
		category.Losses += score.Losses
	}
	if err := rows.Err(); err != nil {
		fail(c, http.StatusInternalServerError, "读取战绩失败")
		return
	}
	categories := make([]categoryRecord, 0, len(byCategory))
	for _, category := range byCategory {
		categories = append(categories, *category)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].CategoryID < categories[j].CategoryID })
	c.Header("Cache-Control", "no-store")
	ok(c, gin.H{
		"user": gin.H{"id": user.ID, "display_name": user.DisplayName, "avatar_url": user.AvatarURL,
			"category_id": user.CategoryID, "category_name": user.CategoryName, "category_icon": user.CategoryIcon},
		"overall": overall, "ace": ace, "by_category": categories,
	})
}
