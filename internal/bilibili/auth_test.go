package bilibili

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestDownloadImageSendsBilibiliHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Referer"); got != "https://www.bilibili.com/" {
			t.Fatalf("Referer = %q", got)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("cover"))
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "cover")
	if err := NewClient(server.Client()).DownloadImage(context.Background(), server.URL, target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "cover" {
		t.Fatalf("cover content = %q", content)
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

func TestGetFavoriteFoldersUsesLoginMidAndCookie(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Referer"); got != "https://www.bilibili.com/" {
			t.Fatalf("Referer = %q", got)
		}
		if cookie, err := r.Cookie("SESSDATA"); err != nil || cookie.Value != "sess" {
			t.Fatalf("SESSDATA cookie = %+v, %v", cookie, err)
		}
		switch r.URL.Path {
		case "/x/web-interface/nav":
			return jsonResponse(`{"code":0,"message":"0","data":{"isLogin":true,"mid":123,"uname":"u","face":""}}`), nil
		case "/x/v3/fav/folder/created/list":
			if r.URL.Query().Get("up_mid") != "123" {
				t.Fatalf("up_mid = %q", r.URL.Query().Get("up_mid"))
			}
			return jsonResponse(`{"code":0,"message":"0","data":{"list":[{"id":456,"title":"watch later"}]}}`), nil
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
			return nil, nil
		}
	})})

	folders, err := client.GetFavoriteFolders(context.Background(), "sess")
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].ID != 456 || folders[0].Title != "watch later" {
		t.Fatalf("unexpected folders: %+v", folders)
	}
}

func TestGetFavoriteResources(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/x/v3/fav/resource/list" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("media_id") != "456" {
			t.Fatalf("media_id = %q", r.URL.Query().Get("media_id"))
		}
		return jsonResponse(`{"code":0,"message":"0","data":{"medias":[{"id":7,"type":2,"title":"video","bvid":"BV1xx411c7mD"}]}}`), nil
	})})

	medias, err := client.GetFavoriteResources(context.Background(), "sess", 456)
	if err != nil {
		t.Fatal(err)
	}
	if len(medias) != 1 || medias[0].ID != 7 || medias[0].Type != 2 || medias[0].Bvid == "" {
		t.Fatalf("unexpected medias: %+v", medias)
	}
}

func TestDeleteFavoriteResourceSendsCSRFAndResource(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/x/v3/fav/resource/batch-del" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, want := range []string{`name="resources"`, "7:2", `name="media_id"`, "456", `name="csrf"`, "csrf"} {
			if !strings.Contains(text, want) {
				t.Fatalf("body missing %q: %s", want, text)
			}
		}
		return jsonResponse(`{"code":0,"message":"0","data":{}}`), nil
	})})

	err := client.DeleteFavoriteResource(context.Background(), "sess", "csrf", 456, FavoriteMedia{ID: 7, Type: 2, Bvid: "BV1xx411c7mD"})
	if err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
