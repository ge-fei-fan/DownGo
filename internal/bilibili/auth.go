package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	qrcodeGenerateURL = "https://passport.bilibili.com/x/passport-login/web/qrcode/generate"
	qrcodePollURL     = "https://passport.bilibili.com/x/passport-login/web/qrcode/poll?qrcode_key="
	navURL            = "https://api.bilibili.com/x/web-interface/nav"
	spaceAccInfoURL   = "http://api.bilibili.com/x/space/acc/info"
	userAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36 Edg/123.0.0.0"
)

var ErrNotLoggedIn = errors.New("bilibili not logged in")

type Client struct {
	http *http.Client
}

type QRCode struct {
	URL string `json:"url"`
	Key string `json:"qrcodeKey"`
}

type PollResult struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	URL     string    `json:"-"`
	Tokens  Tokens    `json:"-"`
	Profile *Profile  `json:"profile,omitempty"`
	LoginAt time.Time `json:"loginAt,omitempty"`
}

type Tokens struct {
	Sessdata string
	BiliJct  string
}

type Profile struct {
	Mid   int    `json:"mid"`
	Uname string `json:"uname"`
	Face  string `json:"face"`
}

type SpaceInfo struct {
	Mid          int    `json:"mid"`
	Name         string `json:"name"`
	Sex          string `json:"sex"`
	Face         string `json:"face"`
	Sign         string `json:"sign"`
	Level        int    `json:"level"`
	SeniorMember int    `json:"is_senior_member"`
	Vip          Vip    `json:"vip"`
}

type Vip struct {
	Type    int      `json:"type"`
	Status  int      `json:"status"`
	DueDate int64    `json:"due_date"`
	Label   VipLabel `json:"label"`
}

type VipLabel struct {
	Text string `json:"text"`
}

type apiResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type qrcodeData struct {
	URL       string `json:"url"`
	QRCodeKey string `json:"qrcode_key"`
}

type pollData struct {
	URL     string `json:"url"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type navData struct {
	IsLogin bool   `json:"isLogin"`
	Mid     int    `json:"mid"`
	Uname   string `json:"uname"`
	Face    string `json:"face"`
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{http: client}
}

func (c *Client) GenerateQRCode(ctx context.Context) (QRCode, error) {
	var resp apiResponse[qrcodeData]
	if err := c.getJSON(ctx, qrcodeGenerateURL, "", &resp); err != nil {
		return QRCode{}, err
	}
	if resp.Code != 0 {
		return QRCode{}, errors.New(resp.Message)
	}
	if resp.Data.URL == "" || resp.Data.QRCodeKey == "" {
		return QRCode{}, errors.New("Bilibili 未返回二维码信息")
	}
	return QRCode{URL: resp.Data.URL, Key: resp.Data.QRCodeKey}, nil
}

func (c *Client) PollQRCode(ctx context.Context, key string) (PollResult, error) {
	if key == "" {
		return PollResult{}, errors.New("缺少二维码 key")
	}
	var resp apiResponse[pollData]
	if err := c.getJSON(ctx, qrcodePollURL+url.QueryEscape(key), "", &resp); err != nil {
		return PollResult{}, err
	}
	if resp.Code != 0 {
		return PollResult{}, errors.New(resp.Message)
	}

	result := PollResult{Code: resp.Data.Code, Message: resp.Data.Message, URL: resp.Data.URL}
	if result.Code != 0 {
		return result, nil
	}

	tokens, err := ExtractTokens(resp.Data.URL)
	if err != nil {
		return PollResult{}, err
	}
	profile, err := c.CheckLogin(ctx, tokens.Sessdata)
	if err != nil {
		return PollResult{}, err
	}
	result.Tokens = tokens
	result.Profile = &profile
	result.LoginAt = time.Now().UTC()
	return result, nil
}

func (c *Client) CheckLogin(ctx context.Context, sessdata string) (Profile, error) {
	var resp apiResponse[navData]
	if err := c.getJSON(ctx, navURL, sessdata, &resp); err != nil {
		return Profile{}, err
	}
	if resp.Code != 0 {
		return Profile{}, errors.New(resp.Message)
	}
	if !resp.Data.IsLogin {
		return Profile{}, ErrNotLoggedIn
	}
	return Profile{Mid: resp.Data.Mid, Uname: resp.Data.Uname, Face: resp.Data.Face}, nil
}

func (c *Client) GetSpaceInfo(ctx context.Context, sessdata string, mid int) (SpaceInfo, error) {
	return c.getSpaceInfoURL(ctx, sessdata, mid, spaceAccInfoURL)
}

func (c *Client) DownloadAvatar(ctx context.Context, avatarURL string, targetPath string) error {
	if avatarURL == "" {
		return errors.New("Bilibili 头像地址为空")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	req, err := c.newRequest(ctx, avatarURL, "")
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Bilibili 头像返回 HTTP %d", res.StatusCode)
	}
	contentType := res.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return fmt.Errorf("Bilibili 头像内容类型无效：%s", contentType)
	}

	tmp, err := os.CreateTemp(filepath.Dir(targetPath), filepath.Base(targetPath)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	written, err := io.Copy(tmp, res.Body)
	if err != nil {
		return err
	}
	if written == 0 {
		return errors.New("Bilibili 头像内容为空")
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

func (c *Client) getSpaceInfoURL(ctx context.Context, sessdata string, mid int, baseURL string) (SpaceInfo, error) {
	if mid <= 0 {
		return SpaceInfo{}, errors.New("缺少 Bilibili mid")
	}
	var resp apiResponse[SpaceInfo]
	targetURL := fmt.Sprintf("%s?mid=%d", baseURL, mid)
	if err := c.getJSON(ctx, targetURL, sessdata, &resp); err != nil {
		return SpaceInfo{}, err
	}
	if resp.Code != 0 {
		return SpaceInfo{}, errors.New(resp.Message)
	}
	return resp.Data, nil
}

func ExtractTokens(rawURL string) (Tokens, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Tokens{}, err
	}
	query := parsed.Query()
	tokens := Tokens{
		Sessdata: query.Get("SESSDATA"),
		BiliJct:  query.Get("bili_jct"),
	}
	if tokens.Sessdata == "" || tokens.BiliJct == "" {
		return Tokens{}, errors.New("Bilibili 登录结果缺少 SESSDATA 或 bili_jct")
	}
	return tokens, nil
}

func (c *Client) getJSON(ctx context.Context, targetURL string, sessdata string, out any) error {
	req, err := c.newRequest(ctx, targetURL, sessdata)
	if err != nil {
		return err
	}

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Bilibili 返回 HTTP %d: %s", res.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func (c *Client) newRequest(ctx context.Context, targetURL string, sessdata string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	if sessdata != "" {
		req.AddCookie(&http.Cookie{Name: "SESSDATA", Value: sessdata})
	}
	return req, nil
}
