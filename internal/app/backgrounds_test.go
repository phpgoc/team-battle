package app

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *workflowFixture) backgroundForm(t *testing.T, user int, method, path, name string, image []byte, want int) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	if image != nil {
		file, err := writer.CreateFormFile("image", "background.png")
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
	if user > 0 {
		request.AddCookie(&http.Cookie{Name: "tb_session", Value: f.tokens[user]})
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("背景管理请求状态错误: %d %s", response.Code, response.Body.String())
	}
	return response
}

func TestBackgroundManagementAndPublicTournamentSelection(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	image, err := demoFS.ReadFile("testdata/demo/zerg.png")
	if err != nil {
		t.Fatal(err)
	}
	f.backgroundForm(t, 2, http.MethodPost, "/api/site-settings/backgrounds", "比赛背景", image, http.StatusForbidden)
	f.backgroundForm(t, 1, http.MethodPost, "/api/site-settings/backgrounds", "比赛背景", image, http.StatusOK)
	backgroundID := f.scalar(t, `SELECT id FROM backgrounds WHERE name='比赛背景'`)
	var storedPath string
	var storedSize, storedBytes int
	if err := f.app.db.QueryRow(`SELECT file_path,image_size,length(image_data) FROM backgrounds WHERE id=?`, backgroundID).Scan(&storedPath, &storedSize, &storedBytes); err != nil {
		t.Fatal(err)
	}
	if storedSize != len(image) || storedBytes != 0 {
		t.Fatalf("背景文件本体应放在文件系统: size=%d blob=%d", storedSize, storedBytes)
	}
	storedFile := filepath.Join(f.app.backgroundDir(), storedPath)
	if _, err := os.Stat(storedFile); err != nil {
		t.Fatalf("背景文件未写入上传目录: %v", err)
	}
	list := f.request(t, 0, http.MethodGet, "/api/backgrounds", nil, http.StatusOK)
	if !bytes.Contains(list, []byte(`"name":"比赛背景"`)) {
		t.Fatalf("访客无法读取可用背景列表: %s", list)
	}
	imageURL := fmt.Sprintf("/api/backgrounds/%d/image", backgroundID)
	if got := f.request(t, 0, http.MethodGet, imageURL, nil, http.StatusOK); !bytes.Equal(got, image) {
		t.Fatal("公开背景图片接口没有返回上传内容")
	}
	path := fmt.Sprintf("/api/tournaments/%d/background", id)
	// 访客、队长、普通选手及其他赛事的管理员均不能修改赞助背景。
	for _, user := range []int{0, 3, 4, 9} {
		f.request(t, user, http.MethodPatch, path, map[string]any{"background_id": backgroundID}, http.StatusForbidden)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND background_id IS NULL`, id)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM tournament_actions WHERE tournament_id=? AND action='tournament_background_updated'`, id)
	f.request(t, 2, http.MethodPatch, path, map[string]any{"background_id": backgroundID}, http.StatusOK)
	f.request(t, 1, http.MethodPatch, path, map[string]any{"background_id": 0}, http.StatusOK)
	f.request(t, 1, http.MethodPatch, path, map[string]any{"background_id": backgroundID}, http.StatusOK)
	detail := f.request(t, 0, http.MethodGet, fmt.Sprintf("/api/public/tournaments/%d", id), nil, http.StatusOK)
	for _, expected := range []string{fmt.Sprintf(`"background_id":%d`, backgroundID), `"background_name":"比赛背景"`, imageURL} {
		if !bytes.Contains(detail, []byte(expected)) {
			t.Fatalf("公开赛事没有背景字段 %q: %s", expected, detail)
		}
	}
	f.request(t, 2, http.MethodPatch, path, map[string]any{"background_id": backgroundID + 999}, http.StatusBadRequest)
	f.request(t, 1, http.MethodPost, "/api/tournaments/archive", map[string]any{
		"name": "带背景历史赛事", "start_time": "2026-11-17T19", "background_id": backgroundID,
		"player_ids":        []int{3, 4, 5, 6, 7, 8},
		"fixed_assignments": map[string]string{"3": "A", "4": "A", "5": "A", "6": "B", "7": "B", "8": "B"},
		"matches": []map[string]any{
			{"round": 1, "a_id": 3, "b_id": 6, "who_win": "a"},
			{"round": 2, "a_id": 4, "b_id": 7, "who_win": "b"},
			{"round": 3, "a_id": 5, "b_id": 8, "who_win": "a"},
		},
	}, http.StatusOK)
	archivedID := f.scalar(t, `SELECT id FROM tournaments WHERE name='带背景历史赛事'`)
	archived := f.request(t, 0, http.MethodGet, fmt.Sprintf("/api/public/tournaments/%d", archivedID), nil, http.StatusOK)
	if !bytes.Contains(archived, []byte(fmt.Sprintf(`"background_id":%d`, backgroundID))) {
		t.Fatalf("历史赛事没有保留录入时选择的背景: %s", archived)
	}
	replacement, err := demoFS.ReadFile("testdata/demo/terran.png")
	if err != nil {
		t.Fatal(err)
	}
	f.backgroundForm(t, 1, http.MethodPatch, fmt.Sprintf("/api/site-settings/backgrounds/%d", backgroundID), "替换背景", replacement, http.StatusOK)
	if _, err := os.Stat(storedFile); !os.IsNotExist(err) {
		t.Fatal("替换背景后旧文件没有清理")
	}
	if got := f.request(t, 0, http.MethodGet, imageURL, nil, http.StatusOK); !bytes.Equal(got, replacement) {
		t.Fatal("替换背景接口没有读取新文件")
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM backgrounds WHERE id=? AND name='替换背景'`, backgroundID)
	f.backgroundForm(t, 1, http.MethodDelete, fmt.Sprintf("/api/site-settings/backgrounds/%d", backgroundID), "", nil, http.StatusConflict)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM tournaments WHERE background_id=?`, backgroundID)
	if got := f.request(t, 0, http.MethodGet, imageURL, nil, http.StatusOK); !bytes.Equal(got, replacement) {
		t.Fatal("拒绝删除已绑定背景后，图片必须保持可用")
	}

	if _, err := f.app.db.Exec(`UPDATE tournaments SET status=? WHERE id=?`, statusCompleted, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 2, http.MethodPatch, path, map[string]any{"background_id": 0}, http.StatusBadRequest)
	f.request(t, 1, http.MethodPatch, path, map[string]any{"background_id": 0}, http.StatusBadRequest)
}

func TestBackgroundDeletionProtectsEveryBoundTournament(t *testing.T) {
	f := newWorkflowFixture(t)
	image, err := demoFS.ReadFile("testdata/demo/zerg.png")
	if err != nil {
		t.Fatal(err)
	}
	id := f.create(t, "random")
	f.backgroundForm(t, 1, http.MethodPost, "/api/site-settings/backgrounds", "删除保护", image, http.StatusOK)
	backgroundID := f.scalar(t, `SELECT id FROM backgrounds WHERE name='删除保护'`)
	path := fmt.Sprintf("/api/site-settings/backgrounds/%d", backgroundID)
	for _, status := range []string{statusDraft, statusRound1, statusRound2, statusAce, statusFinalConfirm, statusCompleted, statusCancelled} {
		if _, err := f.app.db.Exec(`UPDATE tournaments SET status=?,background_id=? WHERE id=?`, status, backgroundID, id); err != nil {
			t.Fatal(err)
		}
		response := f.backgroundForm(t, 1, http.MethodDelete, path, "", nil, http.StatusConflict)
		if !strings.Contains(response.Body.String(), "背景已绑定赛事，不能删除") {
			t.Fatalf("已绑定背景应说明原因: %s", response.Body.String())
		}
		f.assertCount(t, 1, `SELECT COUNT(*) FROM tournaments WHERE id=? AND background_id=?`, id, backgroundID)
		f.assertCount(t, 1, `SELECT COUNT(*) FROM backgrounds WHERE id=?`, backgroundID)
	}
	list := f.request(t, 0, http.MethodGet, "/api/backgrounds", nil, http.StatusOK)
	if !bytes.Contains(list, []byte(`"in_use":true`)) {
		t.Fatalf("背景目录应标识已绑定: %s", list)
	}
	// 无引用的背景仍可删除，且清理原图；重复删除返回404。
	if _, err := f.app.db.Exec(`UPDATE tournaments SET background_id=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	var filePath string
	if err := f.app.db.QueryRow(`SELECT file_path FROM backgrounds WHERE id=?`, backgroundID).Scan(&filePath); err != nil {
		t.Fatal(err)
	}
	f.backgroundForm(t, 2, http.MethodDelete, path, "", nil, http.StatusForbidden)
	f.backgroundForm(t, 1, http.MethodDelete, path, "", nil, http.StatusOK)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM backgrounds WHERE id=?`, backgroundID)
	if _, err := os.Stat(filepath.Join(f.app.backgroundDir(), filePath)); !os.IsNotExist(err) {
		t.Fatal("删除未绑定背景后必须清理原图")
	}
	f.backgroundForm(t, 1, http.MethodDelete, path, "", nil, http.StatusNotFound)
	f.backgroundForm(t, 1, http.MethodPatch, path, "不存在", nil, http.StatusNotFound)
}

func TestBackgroundUploadRejectsInvalidImage(t *testing.T) {
	f := newWorkflowFixture(t)
	response := f.backgroundForm(t, 1, http.MethodPost, "/api/site-settings/backgrounds", "无效图片", []byte("not an image"), http.StatusBadRequest)
	if !strings.Contains(response.Body.String(), "只支持") {
		t.Fatalf("无效图片应返回格式提示: %s", response.Body.String())
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM backgrounds`)
}

func TestLegacyBackgroundsMoveFromDatabaseToFiles(t *testing.T) {
	f := newWorkflowFixture(t)
	image, err := demoFS.ReadFile("testdata/demo/zerg.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`INSERT INTO backgrounds(name,image_data,image_mime) VALUES('旧背景',?,'image/png')`, image); err != nil {
		t.Fatal(err)
	}
	if err := migrateBackgroundFiles(f.app.db, f.app.storageDir); err != nil {
		t.Fatal(err)
	}
	var filePath string
	var size, blobSize int
	if err := f.app.db.QueryRow(`SELECT file_path,image_size,length(image_data) FROM backgrounds WHERE name='旧背景'`).Scan(&filePath, &size, &blobSize); err != nil {
		t.Fatal(err)
	}
	if size != len(image) || blobSize != 0 {
		t.Fatalf("旧背景元数据迁移错误: path=%q size=%d blob=%d", filePath, size, blobSize)
	}
	data, err := os.ReadFile(filepath.Join(f.app.backgroundDir(), filePath))
	if err != nil || !bytes.Equal(data, image) {
		t.Fatal("旧背景文件迁移后内容不一致")
	}
}
