package app

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// 背景原图保持原样，管理列表使用独立压缩预览，不重复下载大图。
func makeBackgroundThumbnail(data []byte) ([]byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 100_000_000 {
		return nil, errors.New("背景图片无法解码或分辨率过大")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("背景图片无法解码")
	}
	scale := math.Min(1, math.Min(480/float64(config.Width), 270/float64(config.Height)))
	width, height := max(1, int(float64(config.Width)*scale)), max(1, int(float64(config.Height)*scale))
	preview := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(preview, preview.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(preview, preview.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, preview, &jpeg.Options{Quality: 75}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func (a *App) backgroundThumbnail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	// 旧背景首次预览时补齐缩略图，避免迁移时解码全部大图或并发重复生成。
	a.thumbnailMu.Lock()
	defer a.thumbnailMu.Unlock()
	var thumbnail []byte
	if err := a.db.QueryRow(`SELECT thumbnail_data FROM backgrounds WHERE id=?`, id).Scan(&thumbnail); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if len(thumbnail) == 0 {
		var data []byte
		var filePath, updatedAt string
		if err := a.db.QueryRow(`SELECT image_data,file_path,CAST(updated_at AS TEXT) FROM backgrounds WHERE id=?`, id).Scan(&data, &filePath, &updatedAt); err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		if filePath != "" {
			data, err = os.ReadFile(filepath.Join(a.backgroundDir(), filepath.Base(filePath)))
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
		}
		thumbnail, err = makeBackgroundThumbnail(data)
		if err != nil {
			fail(c, http.StatusUnprocessableEntity, err.Error())
			return
		}
		// 生成期间图片可能已被替换，不能把旧预览写入新图片的记录。
		if _, err := a.db.Exec(`UPDATE backgrounds SET thumbnail_data=? WHERE id=? AND updated_at=? AND length(COALESCE(thumbnail_data,X''))=0`, thumbnail, id, updatedAt); err != nil {
			fail(c, http.StatusInternalServerError, "保存背景预览失败")
			return
		}
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, "image/jpeg", thumbnail)
}
