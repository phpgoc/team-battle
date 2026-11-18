package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func largeBackgroundPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 1600; x++ {
			im.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x * y), 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, im); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestBackgroundPreviewPreservesOriginalAndUpdates(t *testing.T) {
	f := newWorkflowFixture(t)
	original := largeBackgroundPNG(t)
	f.backgroundForm(t, 1, "POST", "/api/site-settings/backgrounds", "大图预览", original, 200)
	id := f.scalar(t, `SELECT id FROM backgrounds WHERE name='大图预览'`)
	path := fmt.Sprintf("/api/backgrounds/%d", id)
	preview := f.request(t, 0, "GET", path+"/thumbnail", nil, 200)
	config, format, err := image.DecodeConfig(bytes.NewReader(preview))
	if err != nil || format != "jpeg" || config.Width != 480 || config.Height != 270 || len(preview) >= len(original) {
		t.Fatalf("大图未缩小压缩: %+v %s %v preview=%d original=%d", config, format, err, len(preview), len(original))
	}
	if got := f.request(t, 0, "GET", path+"/image", nil, 200); !bytes.Equal(got, original) {
		t.Fatal("压缩预览修改了原图")
	}
	var storedPath string
	if err := f.app.db.QueryRow(`SELECT file_path FROM backgrounds WHERE id=?`, id).Scan(&storedPath); err != nil {
		t.Fatal(err)
	}
	f.backgroundForm(t, 1, "PATCH", fmt.Sprintf("/api/site-settings/backgrounds/%d", id), "只改名称", nil, 200)
	if _, err := os.Stat(filepath.Join(f.app.backgroundDir(), storedPath)); err != nil {
		t.Fatal("只改名称误删原图", err)
	}
	if got := f.request(t, 0, "GET", path+"/thumbnail", nil, 200); !bytes.Equal(got, preview) {
		t.Fatal("改名丢失了缩略图")
	}
	replacement, err := demoFS.ReadFile("testdata/demo/protoss.png")
	if err != nil {
		t.Fatal(err)
	}
	f.backgroundForm(t, 1, "PATCH", fmt.Sprintf("/api/site-settings/backgrounds/%d", id), "换图", replacement, 200)
	if got := f.request(t, 0, "GET", path+"/thumbnail", nil, 200); bytes.Equal(got, preview) {
		t.Fatal("换图仍返回旧缩略图")
	}
	f.backgroundForm(t, 1, "DELETE", fmt.Sprintf("/api/site-settings/backgrounds/%d", id), "", nil, 200)
	f.request(t, 0, "GET", path+"/thumbnail", nil, 404)
}

func TestLegacyBackgroundPreviewIsGeneratedAndCached(t *testing.T) {
	f := newWorkflowFixture(t)
	imageData := largeBackgroundPNG(t)
	if _, err := f.app.db.Exec(`INSERT INTO backgrounds(name,image_data,image_mime) VALUES('旧图预览',?,'image/png')`, imageData); err != nil {
		t.Fatal(err)
	}
	id := f.scalar(t, `SELECT id FROM backgrounds WHERE name='旧图预览'`)
	if err := migrateBackgroundFiles(f.app.db, f.app.storageDir); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/backgrounds/%d/thumbnail", id)
	first := f.request(t, 0, "GET", path, nil, 200)
	f.assertCount(t, int64(len(first)), `SELECT length(thumbnail_data) FROM backgrounds WHERE id=?`, id)
	var storedPath string
	if err := f.app.db.QueryRow(`SELECT file_path FROM backgrounds WHERE id=?`, id).Scan(&storedPath); err != nil {
		t.Fatal(err)
	}
	// 缓存预览后，即使暂时读不到原图也无需再次打开大图。
	if err := os.Remove(filepath.Join(f.app.backgroundDir(), storedPath)); err != nil {
		t.Fatal(err)
	}
	if next := f.request(t, 0, "GET", path, nil, 200); !bytes.Equal(next, first) {
		t.Fatal("缩略图未持久缓存")
	}
	var list struct {
		Backgrounds []struct {
			ThumbnailURL string `json:"thumbnail_url"`
			ImageURL     string `json:"image_url"`
		} `json:"backgrounds"`
	}
	if err := json.Unmarshal(f.request(t, 0, "GET", "/api/backgrounds", nil, 200), &list); err != nil || len(list.Backgrounds) != 1 || list.Backgrounds[0].ThumbnailURL == "" || list.Backgrounds[0].ThumbnailURL == list.Backgrounds[0].ImageURL {
		t.Fatal("背景目录缺少独立缩略图地址", list, err)
	}
}

func TestBackgroundPreviewFormatsAndInvalidData(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for _, format := range []string{"png", "gif", "jpeg", "webp"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			switch format {
			case "png":
				_ = png.Encode(&encoded, source)
			case "gif":
				_ = gif.Encode(&encoded, source, nil)
			case "jpeg":
				_ = jpeg.Encode(&encoded, source, nil)
			case "webp":
				data, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
				if err != nil {
					t.Fatal(err)
				}
				encoded.Write(data)
			}
			preview, err := makeBackgroundThumbnail(encoded.Bytes())
			if err != nil {
				t.Fatal("支持的图片无法生成预览", err)
			}
			config, _, err := image.DecodeConfig(bytes.NewReader(preview))
			if err != nil || config.Width > 80 || config.Height > 40 {
				t.Fatalf("小图不应放大: %+v %v", config, err)
			}
		})
	}
	f := newWorkflowFixture(t)
	broken := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	f.backgroundForm(t, 1, http.MethodPost, "/api/site-settings/backgrounds", "损坏图片", broken, 400)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM backgrounds`)
	f.request(t, 0, "GET", "/api/backgrounds/invalid/thumbnail", nil, 400)
}
