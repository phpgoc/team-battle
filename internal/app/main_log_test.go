package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenOperationLoggerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "operations.log")
	logger, file, err := openOperationLogger(path, "en")
	if err != nil {
		t.Fatal(err)
	}
	if logger == nil || file == nil {
		t.Fatal("文件日志未创建")
	}
	logger.logEvent("tester##1", "login_failed", "")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `tag="Sign-in failed"`) {
		t.Fatalf("文件日志内容不正确: %s", data)
	}
}

func TestOpenOperationLoggerQuietAndStdout(t *testing.T) {
	quiet, file, err := openOperationLogger("", "zh")
	if err != nil || quiet != nil || file != nil {
		t.Fatalf("静默日志返回值不正确: logger=%v file=%v err=%v", quiet, file, err)
	}
	stdout, file, err := openOperationLogger("stdout", "zh")
	if err != nil || stdout == nil || file != nil {
		t.Fatalf("标准输出日志返回值不正确: logger=%v file=%v err=%v", stdout, file, err)
	}
}
