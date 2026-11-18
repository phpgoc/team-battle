package app

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	backgrounds, err := a.loadBackgrounds()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取背景失败")
		return
	}
	ok(c, gin.H{"categories": categories, "backgrounds": backgrounds})
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
	backgrounds, err := a.loadBackgrounds()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取背景失败")
		return
	}
	ok(c, gin.H{"tournament_names": names, "categories": categories, "backgrounds": backgrounds})
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
	if _, err := a.db.Exec(`INSERT INTO site_tournament_names(name,sort_order) VALUES(?,COALESCE((SELECT MAX(sort_order)+1 FROM site_tournament_names),0))`, strings.TrimSpace(req.Name)); err != nil {
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
	if _, err := a.db.Exec(`UPDATE site_tournament_names SET name=? WHERE id=?`, strings.TrimSpace(req.Name), id); err != nil {
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
	if _, err := a.db.Exec(`DELETE FROM site_tournament_names WHERE id=?`, id); err != nil {
		fail(c, http.StatusInternalServerError, "删除常用赛事名失败")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) createCategory(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	name, data, mimeType, err := readCategoryForm(c, true)
	if strings.TrimSpace(name) == "" {
		fail(c, http.StatusBadRequest, "分类名不能为空")
		return
	}
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := a.db.Exec(`INSERT INTO categories(name,icon,icon_data,icon_mime,sort_order) VALUES(?,?,?, ?,COALESCE((SELECT MAX(sort_order)+1 FROM categories),0))`, strings.TrimSpace(name), "", data, mimeType); err != nil {
		fail(c, http.StatusBadRequest, "分类已存在或无法创建")
		return
	}
	a.invalidateCategoryDirectory()
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
	name, data, mimeType, err := readCategoryForm(c, false)
	if strings.TrimSpace(name) == "" {
		fail(c, http.StatusBadRequest, "分类名不能为空")
		return
	}
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	var updateErr error
	// 图片地址含更新时间作为版本号，使用纳秒避免同一秒内替换图片后仍命中旧缓存。
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if len(data) > 0 {
		_, updateErr = a.db.Exec(`UPDATE categories SET name=?,icon='',icon_data=?,icon_mime=?,updated_at=? WHERE id=?`, strings.TrimSpace(name), data, mimeType, updatedAt, id)
	} else {
		_, updateErr = a.db.Exec(`UPDATE categories SET name=?,updated_at=? WHERE id=?`, strings.TrimSpace(name), updatedAt, id)
	}
	if updateErr != nil {
		fail(c, http.StatusBadRequest, "分类已存在或无法修改")
		return
	}
	a.invalidateCategoryDirectory()
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
	a.invalidateCategoryDirectory()
	ok(c, gin.H{"ok": true})
}

func (a *App) categoryIcon(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "分类编号无效")
		return
	}
	var data []byte
	var mimeType string
	if err := a.db.QueryRow(`SELECT icon_data,icon_mime FROM categories WHERE id=?`, id).Scan(&data, &mimeType); err != nil || len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, mimeType, data)
}

func readCategoryForm(c *gin.Context, requiredIcon bool) (string, []byte, string, error) {
	name := strings.TrimSpace(c.PostForm("name"))
	file, header, err := c.Request.FormFile("icon")
	if err != nil {
		if err == http.ErrMissingFile {
			if !requiredIcon {
				return name, nil, "", nil
			}
			return name, nil, "", fmt.Errorf("请上传分类图标图片")
		}
		return name, nil, "", fmt.Errorf("分类图标无效")
	}
	defer file.Close()
	if header.Size > 2*1024*1024 {
		return name, nil, "", fmt.Errorf("分类图标不能超过2MB")
	}
	data, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil || len(data) == 0 || len(data) > 2*1024*1024 {
		return name, nil, "", fmt.Errorf("分类图标无效")
	}
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return name, nil, "", fmt.Errorf("分类图标只支持 JPG、PNG、GIF 或 WebP")
	}
	return name, data, mimeType, nil
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
	rows, err := a.db.Query(`SELECT id,name FROM site_tournament_names ORDER BY sort_order,id`)
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
	return a.cachedCategories()
}
