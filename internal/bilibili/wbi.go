package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49,
	33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40,
	61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11,
	36, 20, 34, 44, 52,
}

type wbiCache struct {
	mu        sync.Mutex
	imgKey    string
	subKey    string
	updatedAt time.Time
}

var globalWBICache wbiCache

type wbiNavData struct {
	WBIImg struct {
		ImgURL string `json:"img_url"`
		SubURL string `json:"sub_url"`
	} `json:"wbi_img"`
}

func (c *Client) signWBI(ctx context.Context, rawURL string, sessdata string) (string, error) {
	imgKey, subKey, err := c.getWBIKeys(ctx, sessdata)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	params := map[string]string{}
	for key, values := range parsed.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	parsed.RawQuery = encodeWBI(params, imgKey, subKey).Encode()
	return parsed.String(), nil
}

func (c *Client) getWBIKeys(ctx context.Context, sessdata string) (string, string, error) {
	globalWBICache.mu.Lock()
	defer globalWBICache.mu.Unlock()
	if globalWBICache.imgKey != "" && globalWBICache.subKey != "" && time.Since(globalWBICache.updatedAt) < 10*time.Minute {
		return globalWBICache.imgKey, globalWBICache.subKey, nil
	}

	var resp apiResponse[wbiNavData]
	if err := c.getJSON(ctx, navURL, sessdata, &resp); err != nil {
		return "", "", err
	}
	if resp.Code != 0 {
		return "", "", ErrNotLoggedIn
	}
	imgKey := keyFromURL(resp.Data.WBIImg.ImgURL)
	subKey := keyFromURL(resp.Data.WBIImg.SubURL)
	if imgKey == "" || subKey == "" {
		return "", "", errors.New("Bilibili WBI key 为空")
	}
	globalWBICache.imgKey = imgKey
	globalWBICache.subKey = subKey
	globalWBICache.updatedAt = time.Now()
	return imgKey, subKey, nil
}

func encodeWBI(params map[string]string, imgKey string, subKey string) url.Values {
	params["wts"] = strconv.FormatInt(time.Now().Unix(), 10)
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	query := url.Values{}
	for _, key := range keys {
		query.Set(key, sanitizeWBIValue(params[key]))
	}
	hash := md5.Sum([]byte(query.Encode() + getMixinKey(imgKey+subKey)))
	query.Set("w_rid", hex.EncodeToString(hash[:]))
	return query
}

func getMixinKey(orig string) string {
	var builder strings.Builder
	for _, index := range mixinKeyEncTab {
		if index < len(orig) {
			builder.WriteByte(orig[index])
		}
	}
	value := builder.String()
	if len(value) > 32 {
		return value[:32]
	}
	return value
}

func sanitizeWBIValue(value string) string {
	replacer := strings.NewReplacer("!", "", "'", "", "(", "", ")", "", "*", "")
	return replacer.Replace(value)
}

func keyFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := path.Base(parsed.Path)
	return strings.TrimSuffix(base, path.Ext(base))
}
