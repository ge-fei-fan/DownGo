package bilibili

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	viewURL    = "https://api.bilibili.com/x/web-interface/view"
	playURL    = "https://api.bilibili.com/x/player/wbi/playurl"
	defaultQn  = "127"
	defaultFnv = "3216"
)

var bvidPattern = regexp.MustCompile(`(?i)BV[0-9A-Za-z]+`)

type VideoInfo struct {
	Bvid     string
	Title    string
	Pic      string
	Duration int64
	Pages    []Page
}

type Page struct {
	CID      int64  `json:"cid"`
	Page     int    `json:"page"`
	Part     string `json:"part"`
	Duration int64  `json:"duration"`
}

type PlayInfo struct {
	AcceptDescription []string        `json:"accept_description"`
	AcceptQuality     []int64         `json:"accept_quality"`
	SupportFormats    []SupportFormat `json:"support_formats"`
	Dash              Dash            `json:"dash"`
}

type SupportFormat struct {
	Quality        int    `json:"quality"`
	NewDescription string `json:"new_description"`
}

type Dash struct {
	Video []DashVideo `json:"video"`
	Audio []DashAudio `json:"audio"`
}

type DashVideo struct {
	ID        int      `json:"id"`
	BaseURL   string   `json:"base_url"`
	BackupURL []string `json:"backupUrl"`
	Bandwidth int64    `json:"bandwidth"`
	Codecs    string   `json:"codecs"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
}

type DashAudio struct {
	BaseURL   string   `json:"base_url"`
	BackupURL []string `json:"backupUrl"`
	Bandwidth int64    `json:"bandwidth"`
}

type SelectedStreams struct {
	Video        DashVideo
	Audio        DashAudio
	QualityLabel string
}

type viewData struct {
	Bvid     string `json:"bvid"`
	CID      int64  `json:"cid"`
	Pic      string `json:"pic"`
	Title    string `json:"title"`
	Duration int64  `json:"duration"`
	Pages    []Page `json:"pages"`
}

func ParseVideoURL(raw string) (string, int, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", 0, err
	}
	bvid := bvidPattern.FindString(parsed.Path)
	if bvid == "" {
		bvid = bvidPattern.FindString(raw)
	}
	if bvid == "" {
		return "", 0, errors.New("未识别到 Bilibili BV 号")
	}
	page := 0
	if value := parsed.Query().Get("p"); value != "" {
		if parsedPage, err := strconv.Atoi(value); err == nil && parsedPage > 0 {
			page = parsedPage
		}
	}
	return strings.ToUpper(bvid[:2]) + bvid[2:], page, nil
}

func (c *Client) GetVideoInfo(ctx context.Context, sessdata string, rawURL string) (VideoInfo, int, error) {
	bvid, requestedPage, err := ParseVideoURL(rawURL)
	if err != nil {
		return VideoInfo{}, 0, err
	}
	targetURL := fmt.Sprintf("%s?bvid=%s", viewURL, url.QueryEscape(bvid))
	var resp apiResponse[viewData]
	if err := c.getJSON(ctx, targetURL, sessdata, &resp); err != nil {
		return VideoInfo{}, 0, err
	}
	if resp.Code != 0 {
		return VideoInfo{}, 0, errors.New(resp.Message)
	}
	pages := resp.Data.Pages
	if len(pages) == 0 && resp.Data.CID > 0 {
		pages = []Page{{CID: resp.Data.CID, Page: 1, Part: resp.Data.Title, Duration: resp.Data.Duration}}
	}
	if len(pages) == 0 {
		return VideoInfo{}, 0, errors.New("Bilibili 未返回分 P 信息")
	}
	return VideoInfo{Bvid: resp.Data.Bvid, Title: resp.Data.Title, Pic: resp.Data.Pic, Duration: resp.Data.Duration, Pages: pages}, requestedPage, nil
}

func (c *Client) GetPlayInfo(ctx context.Context, sessdata string, bvid string, cid int64) (PlayInfo, error) {
	query := url.Values{}
	query.Set("fnver", "0")
	query.Set("fnval", defaultFnv)
	query.Set("fourk", "1")
	query.Set("qn", defaultQn)
	query.Set("bvid", bvid)
	query.Set("cid", strconv.FormatInt(cid, 10))
	signedURL, err := c.signWBI(ctx, playURL+"?"+query.Encode(), sessdata)
	if err != nil {
		return PlayInfo{}, err
	}
	var resp apiResponse[PlayInfo]
	if err := c.getJSON(ctx, signedURL, sessdata, &resp); err != nil {
		return PlayInfo{}, err
	}
	if resp.Code != 0 {
		return PlayInfo{}, errors.New(resp.Message)
	}
	if len(resp.Data.Dash.Video) == 0 || len(resp.Data.Dash.Audio) == 0 {
		return PlayInfo{}, errors.New("Bilibili 未返回可下载 DASH 音视频流")
	}
	return resp.Data, nil
}

func SelectStreams(play PlayInfo) (SelectedStreams, error) {
	if len(play.Dash.Video) == 0 || len(play.Dash.Audio) == 0 {
		return SelectedStreams{}, errors.New("Bilibili 未返回可下载 DASH 音视频流")
	}
	videos := append([]DashVideo(nil), play.Dash.Video...)
	sort.SliceStable(videos, func(i, j int) bool {
		if videos[i].ID != videos[j].ID {
			return videos[i].ID > videos[j].ID
		}
		iAVC := strings.Contains(strings.ToLower(videos[i].Codecs), "avc1")
		jAVC := strings.Contains(strings.ToLower(videos[j].Codecs), "avc1")
		if iAVC != jAVC {
			return iAVC
		}
		return videos[i].Bandwidth > videos[j].Bandwidth
	})
	audios := append([]DashAudio(nil), play.Dash.Audio...)
	sort.SliceStable(audios, func(i, j int) bool {
		return audios[i].Bandwidth > audios[j].Bandwidth
	})
	return SelectedStreams{Video: videos[0], Audio: audios[0], QualityLabel: qualityLabel(play, videos[0])}, nil
}

func qualityLabel(play PlayInfo, video DashVideo) string {
	for _, format := range play.SupportFormats {
		if format.Quality == video.ID && strings.TrimSpace(format.NewDescription) != "" {
			return format.NewDescription
		}
	}
	if video.Height > 0 {
		return strconv.Itoa(video.Height) + "p"
	}
	return strconv.Itoa(video.ID)
}

func StreamURL(primary string, backups []string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	for _, backup := range backups {
		if strings.TrimSpace(backup) != "" {
			return backup
		}
	}
	return ""
}
