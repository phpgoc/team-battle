package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *App) settingsCatalog(c *gin.Context) {
	if _, err := a.currentUser(c); err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	categories, err := a.loadCategories()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取分类失败")
		return
	}
	ok(c, gin.H{"categories": categories})
}

func (a *App) siteSettings(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	names, err := a.loadEventNames()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取常用赛事名失败")
		return
	}
	categories, err := a.loadCategories()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取分类失败")
		return
	}
	ok(c, gin.H{"event_names": names, "categories": categories})
}

func (a *App) createEventName(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	var req catalogNameRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(c, http.StatusBadRequest, "常用赛事名不能为空")
		return
	}
	if _, err := a.db.Exec(`INSERT INTO site_event_names(name,sort_order) VALUES(?,COALESCE((SELECT MAX(sort_order)+1 FROM site_event_names),0))`, strings.TrimSpace(req.Name)); err != nil {
		fail(c, http.StatusBadRequest, "常用赛事名已存在或无法创建")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) updateEventName(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("nameID"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "名称编号无效")
		return
	}
	var req catalogNameRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(c, http.StatusBadRequest, "常用赛事名不能为空")
		return
	}
	if _, err := a.db.Exec(`UPDATE site_event_names SET name=? WHERE id=?`, strings.TrimSpace(req.Name), id); err != nil {
		fail(c, http.StatusBadRequest, "常用赛事名已存在或无法修改")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) deleteEventName(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("nameID"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "名称编号无效")
		return
	}
	if _, err := a.db.Exec(`DELETE FROM site_event_names WHERE id=?`, id); err != nil {
		fail(c, http.StatusInternalServerError, "删除常用赛事名失败")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) createCategory(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(c, http.StatusBadRequest, "分类名不能为空")
		return
	}
	if _, err := a.db.Exec(`INSERT INTO categories(name,icon,sort_order) VALUES(?,?,COALESCE((SELECT MAX(sort_order)+1 FROM categories),0))`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Icon)); err != nil {
		fail(c, http.StatusBadRequest, "分类已存在或无法创建")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) updateCategory(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("categoryID"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "分类编号无效")
		return
	}
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(c, http.StatusBadRequest, "分类名不能为空")
		return
	}
	if _, err := a.db.Exec(`UPDATE categories SET name=?,icon=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Icon), id); err != nil {
		fail(c, http.StatusBadRequest, "分类已存在或无法修改")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) deleteCategory(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("categoryID"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "分类编号无效")
		return
	}
	if _, err := a.db.Exec(`UPDATE users SET category_id=NULL WHERE category_id=?`, id); err != nil {
		fail(c, http.StatusInternalServerError, "清理分类引用失败")
		return
	}
	if _, err := a.db.Exec(`DELETE FROM categories WHERE id=?`, id); err != nil {
		fail(c, http.StatusInternalServerError, "删除分类失败")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) requireSuperAdmin(c *gin.Context) bool {
	user, err := a.currentUser(c)
	if err != nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return false
	}
	if user.Role != roleSuperAdmin {
		fail(c, http.StatusForbidden, "只有超级管理员可以管理网站设置")
		return false
	}
	return true
}

func (a *App) loadEventNames() ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,name FROM site_event_names ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": id, "name": name})
	}
	return items, rows.Err()
}

func (a *App) loadCategories() ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,name,icon FROM categories ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, icon string
		if err := rows.Scan(&id, &name, &icon); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": id, "name": name, "icon": icon})
	}
	return items, rows.Err()
}
