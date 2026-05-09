package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTokens(t *testing.T) {
	tokens, err := ExtractTokens("https://example.com/callback?SESSDATA=abc%2Cdef&bili_jct=csrf123")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.Sessdata != "abc,def" {
		t.Fatalf("Sessdata = %q", tokens.Sessdata)
	}
	if tokens.BiliJct != "csrf123" {
		t.Fatalf("BiliJct = %q", tokens.BiliJct)
	}
}

func TestDownloadAvatar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Referer"); got != "https://www.bilibili.com/" {
			t.Fatalf("Referer = %q", got)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("avatar"))
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "avatar")
	if err := NewClient(server.Client()).DownloadAvatar(context.Background(), server.URL, target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "avatar" {
		t.Fatalf("avatar content = %q", content)
	}
}

func TestDownloadAvatarRejectsNonImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not image"))
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "avatar")
	if err := NewClient(server.Client()).DownloadAvatar(context.Background(), server.URL, target); err == nil {
		t.Fatal("expected non-image error")
	}
}

func TestExtractTokensRequiresValues(t *testing.T) {
	if _, err := ExtractTokens("https://example.com/callback?SESSDATA=abc"); err == nil {
		t.Fatal("expected missing bili_jct error")
	}
	if _, err := ExtractTokens("https://example.com/callback?bili_jct=csrf123"); err == nil {
		t.Fatal("expected missing SESSDATA error")
	}
}

func TestGetSpaceInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Referer"); got != "https://www.bilibili.com/" {
			t.Fatalf("Referer = %q", got)
		}
		if r.URL.Query().Get("mid") != "123" {
			t.Fatalf("mid = %q", r.URL.Query().Get("mid"))
		}
		_, _ = w.Write([]byte(`{
			"code": 0,
			"message": "0",
			"data": {
				"mid": 123,
				"name": "tester",
				"sex": "保密",
				"face": "https://example.com/face.jpg",
				"sign": "hello",
				"level": 6,
				"is_senior_member": 1,
				"vip": {"type": 2, "status": 1, "due_date": 1893456000000, "label": {"text": "年度大会员"}}
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	info, err := client.getSpaceInfoURL(context.Background(), "sess", 123, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "tester" || info.Face == "" || info.Level != 6 {
		t.Fatalf("unexpected info: %+v", info)
	}
	if info.Vip.Status != 1 || info.Vip.Label.Text != "年度大会员" {
		t.Fatalf("unexpected vip: %+v", info.Vip)
	}
}
