package main

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxBackgroundSize = 8 * 1024 * 1024

// loadBackgrounds 返回背景目录的公开字段，图片本体仍通过独立地址读取。
func (a *App) loadBackgrounds() ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,name,updated_at FROM backgrounds ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, updatedAt string
		if err := rows.Scan(&id, &name, &updatedAt); err != nil {
			return nil, err
		}
		items = append(items, backgroundItem(id, name, updatedAt))
	}
	return items, rows.Err()
}

func backgroundItem(id int64, name, updatedAt string) map[string]any {
	return gin.H{
		"id":        id,
		"name":      name,
		"image_url": "/api/backgrounds/" + strconv.FormatInt(id, 10) + "/image?v=" + url.QueryEscape(updatedAt),
	}
}

func (a *App) listBackgrounds(c *gin.Context) {
	items, err := a.loadBackgrounds()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取背景失败")
		return
	}
	ok(c, gin.H{"backgrounds": items})
}

func (a *App) backgroundImage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	var data []byte
	var mimeType string
	if err := a.db.QueryRow(`SELECT image_data,image_mime FROM backgrounds WHERE id=?`, id).Scan(&data, &mimeType); err != nil || len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, mimeType, data)
}

func (a *App) createBackground(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	name, data, mimeType, err := readBackgroundForm(c, true)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if name == "" {
		fail(c, http.StatusBadRequest, "背景名称不能为空")
		return
	}
	if _, err := a.db.Exec(`INSERT INTO backgrounds(name,image_data,image_mime,sort_order) VALUES(?,?,?,COALESCE((SELECT MAX(sort_order)+1 FROM backgrounds),0))`, name, data, mimeType); err != nil {
		fail(c, http.StatusBadRequest, "背景名称已存在或无法创建")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) updateBackground(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("backgroundID"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "背景编号无效")
		return
	}
	name, data, mimeType, err := readBackgroundForm(c, false)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if name == "" {
		fail(c, http.StatusBadRequest, "背景名称不能为空")
		return
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	var result sql.Result
	var resultErr error
	if len(data) > 0 {
		result, resultErr = a.db.Exec(`UPDATE backgrounds SET name=?,image_data=?,image_mime=?,updated_at=? WHERE id=?`, name, data, mimeType, updatedAt, id)
	} else {
		result, resultErr = a.db.Exec(`UPDATE backgrounds SET name=?,updated_at=? WHERE id=?`, name, updatedAt, id)
	}
	if resultErr != nil {
		fail(c, http.StatusBadRequest, "背景名称已存在或无法修改")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		fail(c, http.StatusNotFound, "背景不存在")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) deleteBackground(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("backgroundID"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "背景编号无效")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除背景失败")
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE events SET background_id=NULL WHERE background_id=?`, id); err != nil {
		fail(c, http.StatusInternalServerError, "清理赛事背景引用失败")
		return
	}
	result, err := tx.Exec(`DELETE FROM backgrounds WHERE id=?`, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除背景失败")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		fail(c, http.StatusNotFound, "背景不存在")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(c, http.StatusInternalServerError, "删除背景失败")
		return
	}
	ok(c, gin.H{"ok": true})
}

func readBackgroundForm(c *gin.Context, requiredImage bool) (string, []byte, string, error) {
	name := strings.TrimSpace(c.PostForm("name"))
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		if err == http.ErrMissingFile && !requiredImage {
			return name, nil, "", nil
		}
		return name, nil, "", fmt.Errorf("请上传背景图片")
	}
	defer file.Close()
	if header.Size > maxBackgroundSize {
		return name, nil, "", fmt.Errorf("背景图片不能超过8MB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBackgroundSize+1))
	if err != nil || len(data) == 0 || len(data) > maxBackgroundSize {
		return name, nil, "", fmt.Errorf("背景图片无效")
	}
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return name, nil, "", fmt.Errorf("背景图片只支持 JPG、PNG、GIF 或 WebP")
	}
	return name, data, mimeType, nil
}
