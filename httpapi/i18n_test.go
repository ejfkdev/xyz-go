package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ejfkdev/xyz-go/langx"
)

// Accept-Language 逐请求语言：框架生成的 HTTP 错误消息按请求语言本地化，
// 无受支持标签时回退进程默认语言（xyz-spec §11.7）。
func TestHTTPAcceptLanguage(t *testing.T) {
	langx.Set(langx.En, nil) // 进程默认英文，确保回退分支确定
	defer langx.Set(langx.En, nil)
	h := mustHandler(t, buildHTTPReg(t))

	// 无 Accept-Language → 回退进程默认（英文）。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/users/alice", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid JSON body") {
		t.Fatalf("default(en): code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Accept-Language: zh-CN → 中文消息。
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/users/alice", strings.NewReader("{bad"))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest || !strings.Contains(rec2.Body.String(), "无效的 JSON 请求体") {
		t.Fatalf("zh: code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// 不受支持的语言 → 回退英文。
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("POST", "/users/alice", strings.NewReader("{bad"))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Accept-Language", "fr-FR,de;q=0.9")
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest || !strings.Contains(rec3.Body.String(), "invalid JSON body") {
		t.Fatalf("unsupported→en: code=%d body=%s", rec3.Code, rec3.Body.String())
	}
}
