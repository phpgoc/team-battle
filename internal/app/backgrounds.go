package app

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxBackgroundSize = 64 * 1024 * 1024

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
	var mimeType, filePath string
	if err := a.db.QueryRow(`SELECT image_data,image_mime,file_path FROM backgrounds WHERE id=?`, id).Scan(&data, &mimeType, &filePath); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if filePath != "" {
		if stored, err := os.ReadFile(filepath.Join(a.backgroundDir(), filepath.Base(filePath))); err == nil {
			data = stored
		}
	}
	if len(data) == 0 {
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
	result, err := a.db.Exec(`INSERT INTO backgrounds(name,image_data,image_mime,file_path,image_size,sort_order) VALUES(?,?,?,?,?,COALESCE((SELECT MAX(sort_order)+1 FROM backgrounds),0))`, name, []byte{}, mimeType, "", len(data))
	if err != nil {
		fail(c, http.StatusBadRequest, "背景名称已存在或无法创建")
		return
	}
	id, _ := result.LastInsertId()
	filePath := backgroundFileName(id, mimeType)
	if err := os.MkdirAll(a.backgroundDir(), 0755); err != nil || os.WriteFile(filepath.Join(a.backgroundDir(), filePath), data, 0644) != nil {
		_, _ = a.db.Exec(`DELETE FROM backgrounds WHERE id=?`, id)
		fail(c, http.StatusInternalServerError, "保存背景图片失败")
		return
	}
	if _, err := a.db.Exec(`UPDATE backgrounds SET file_path=? WHERE id=?`, filePath, id); err != nil {
		_ = os.Remove(filepath.Join(a.backgroundDir(), filePath))
		_, _ = a.db.Exec(`DELETE FROM backgrounds WHERE id=?`, id)
		fail(c, http.StatusInternalServerError, "保存背景图片失败")
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
	oldPath := ""
	_ = a.db.QueryRow(`SELECT file_path FROM backgrounds WHERE id=?`, id).Scan(&oldPath)
	newPath := ""
	if len(data) > 0 {
		filePath := backgroundFileNameVersion(id, mimeType)
		newPath = filePath
		if err := os.MkdirAll(a.backgroundDir(), 0755); err != nil {
			resultErr = err
		} else if err := os.WriteFile(filepath.Join(a.backgroundDir(), filePath), data, 0644); err != nil {
			resultErr = err
		} else {
			result, resultErr = a.db.Exec(`UPDATE backgrounds SET name=?,image_data=?,image_mime=?,file_path=?,image_size=?,updated_at=? WHERE id=?`, name, []byte{}, mimeType, filePath, len(data), updatedAt, id)
		}
	} else {
		result, resultErr = a.db.Exec(`UPDATE backgrounds SET name=?,updated_at=? WHERE id=?`, name, updatedAt, id)
	}
	if resultErr != nil {
		if newPath != "" {
			_ = os.Remove(filepath.Join(a.backgroundDir(), newPath))
		}
		fail(c, http.StatusBadRequest, "背景名称已存在或无法修改")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if newPath != "" {
			_ = os.Remove(filepath.Join(a.backgroundDir(), newPath))
		}
		fail(c, http.StatusNotFound, "背景不存在")
		return
	}
	if oldPath != "" && oldPath != newPath {
		_ = os.Remove(filepath.Join(a.backgroundDir(), filepath.Base(oldPath)))
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
	var filePath string
	_ = tx.QueryRow(`SELECT file_path FROM backgrounds WHERE id=?`, id).Scan(&filePath)
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
	if filePath != "" {
		_ = os.Remove(filepath.Join(a.backgroundDir(), filepath.Base(filePath)))
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) backgroundDir() string {
	root := a.storageDir
	if root == "" {
		root = "."
	}
	return filepath.Join(root, "uploads", "backgrounds")
}

func backgroundFileName(id int64, mimeType string) string {
	return backgroundFileNameWithSuffix(id, mimeType, "")
}

func backgroundFileNameVersion(id int64, mimeType string) string {
	return backgroundFileNameWithSuffix(id, mimeType, fmt.Sprintf("-%d", time.Now().UnixNano()))
}

func backgroundFileNameWithSuffix(id int64, mimeType, suffix string) string {
	ext := ".bin"
	switch mimeType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}
	return fmt.Sprintf("background-%d%s%s", id, suffix, ext)
}

func migrateBackgroundFiles(db *sql.DB, root string) error {
	rows, err := db.Query(`SELECT id,image_data,image_mime,file_path FROM backgrounds WHERE length(image_data)>0`)
	if err != nil {
		return err
	}
	type legacyBackground struct {
		id       int64
		data     []byte
		mimeType string
		filePath string
	}
	items := []legacyBackground{}
	for rows.Next() {
		var item legacyBackground
		if err := rows.Scan(&item.id, &item.data, &item.mimeType, &item.filePath); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(items) == 0 {
		return nil
	}
	if root == "" {
		root = "."
	}
	directory := filepath.Join(root, "uploads", "backgrounds")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	for _, item := range items {
		filePath := filepath.Base(item.filePath)
		if filePath == "." || filePath == "" {
			filePath = backgroundFileName(item.id, item.mimeType)
		}
		if err := os.WriteFile(filepath.Join(directory, filePath), item.data, 0644); err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE backgrounds SET file_path=?,image_size=?,image_data=zeroblob(0) WHERE id=?`, filePath, len(item.data), item.id); err != nil {
			return err
		}
	}
	return nil
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
		return name, nil, "", fmt.Errorf("背景图片不能超过64MB")
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
