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

// 战绩只提供公开资料，访客和本人共用同一份统计。
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
	record, err := loadPlayerRecord(a.db, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取战绩失败")
		return
	}
	// 按对手当前分类汇总，调整分类后无需回写战绩。
	byCategory := map[int64]*categoryRecord{}
	for opponentID, score := range record.opponents {
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
		category.Wins += score[0]
		category.Losses += score[1]
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
		"overall": record.overall, "ace": record.ace, "team_battle": record.team, "by_category": categories,
	})
}
