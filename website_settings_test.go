package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (f *workflowFixture) categoryForm(t *testing.T, user int, method, path, name string, image []byte, want int) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	if image != nil {
		file, err := writer.CreateFormFile("icon", "icon.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(image); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(&http.Cookie{Name: "tb_session", Value: f.tokens[user]})
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("分类上传状态错误: %d %s", response.Code, response.Body.String())
	}
	return response
}

func TestCategoryImageReplacementInvalidatesBothCaches(t *testing.T) {
	f := newWorkflowFixture(t)
	first, err := demoFS.ReadFile("web/demo/zerg.png")
	if err != nil {
		t.Fatal(err)
	}
	second, err := demoFS.ReadFile("web/demo/terran.png")
	if err != nil {
		t.Fatal(err)
	}
	f.categoryForm(t, 3, "POST", "/api/site-settings/categories", "新分类", first, 403)
	f.categoryForm(t, 1, "POST", "/api/site-settings/categories", "新分类", first, 200)
	id := f.scalar(t, `SELECT id FROM categories WHERE name='新分类'`)
	if _, err := f.app.db.Exec(`UPDATE users SET category_id=? WHERE id=3`, id); err != nil {
		t.Fatal(err)
	}
	f.app.invalidateCategoryDirectory()
	before, err := f.app.userByID(3)
	if err != nil || before.CategoryIcon == "" {
		t.Fatalf("未加载账号分类图片: %+v %v", before, err)
	}
	path := fmt.Sprintf("/api/site-settings/categories/%d", id)
	// 连续替换两次而不等待跨秒，目录和浏览器图片地址都应更新。
	f.categoryForm(t, 1, "PATCH", path, "已改名", second, 200)
	middle, err := f.app.userByID(3)
	if err != nil || middle.CategoryIcon == before.CategoryIcon || middle.CategoryName != "已改名" {
		t.Fatalf("首次替换未更新目录缓存或图片地址: %+v %v", middle, err)
	}
	f.categoryForm(t, 1, "PATCH", path, "已改名", first, 200)
	after, err := f.app.userByID(3)
	if err != nil || after.CategoryIcon == middle.CategoryIcon {
		t.Fatalf("连续替换未更新图片版本: %+v %v", after, err)
	}
	image := f.request(t, 0, "GET", after.CategoryIcon, nil, 200)
	if !bytes.Equal(image, first) {
		t.Fatal("图片接口未返回最新内容")
	}
	// 错误图片不能覆盖已有分类名、图片或缓存版本。
	response := f.categoryForm(t, 1, "PATCH", path, "不能保存的名称", []byte("invalid image"), 400)
	var failure struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || !strings.Contains(failure.Error, "只支持") {
		t.Fatalf("应该明确提示图片格式错误: %s", response.Body.String())
	}
	unchanged, err := f.app.userByID(3)
	if err != nil || unchanged.CategoryName != after.CategoryName || unchanged.CategoryIcon != after.CategoryIcon {
		t.Fatalf("无效上传不应保存任何字段: %+v %v", unchanged, err)
	}
	response = f.categoryForm(t, 1, "POST", "/api/site-settings/categories", "缺少图片", nil, 400)
	if !strings.Contains(response.Body.String(), "请上传分类图标图片") {
		t.Fatalf("缺少图片的提示错误: %s", response.Body.String())
	}
}
