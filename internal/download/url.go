package download

import (
	"errors"
	"net/url"
	"strings"
)

var allowedYouTubeHosts = map[string]struct{}{
	"youtube.com":     {},
	"www.youtube.com": {},
	"m.youtube.com":   {},
	"youtu.be":        {},
}

func normalizeYouTubeURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("仅支持 YouTube 链接，请输入 youtube.com 或 youtu.be 链接")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", errors.New("仅支持 YouTube 链接，请输入 youtube.com 或 youtu.be 链接")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("仅支持 YouTube 链接，请输入 youtube.com 或 youtu.be 链接")
	}

	host := strings.ToLower(parsed.Hostname())
	if _, ok := allowedYouTubeHosts[host]; !ok {
		return "", errors.New("仅支持 YouTube 链接，请输入 youtube.com 或 youtu.be 链接")
	}

	return trimmed, nil
}
