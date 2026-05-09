package download

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	"example.com/downgo/internal/domain"
)

const unsupportedURLMessage = "仅支持 YouTube 或 Bilibili 链接，请输入 youtube.com、youtu.be、bilibili.com 或 b23.tv 链接"

var (
	allowedYouTubeHosts = map[string]struct{}{
		"youtube.com":     {},
		"www.youtube.com": {},
		"m.youtube.com":   {},
		"youtu.be":        {},
	}
	allowedBilibiliHosts = map[string]struct{}{
		"bilibili.com":     {},
		"www.bilibili.com": {},
		"m.bilibili.com":   {},
		"b23.tv":           {},
	}
	urlPattern = regexp.MustCompile(`https?://[^\s]+`)
)

type normalizedSource struct {
	URL      string
	Platform string
}

func normalizeSourceURL(raw string) (normalizedSource, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return normalizedSource{}, errors.New(unsupportedURLMessage)
	}
	if match := urlPattern.FindString(trimmed); match != "" {
		trimmed = strings.TrimRight(match, "，。),]\"'")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return normalizedSource{}, errors.New(unsupportedURLMessage)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return normalizedSource{}, errors.New(unsupportedURLMessage)
	}

	host := strings.ToLower(parsed.Hostname())
	if _, ok := allowedYouTubeHosts[host]; ok {
		return normalizedSource{URL: trimmed, Platform: domain.PlatformYouTube}, nil
	}
	if _, ok := allowedBilibiliHosts[host]; ok {
		return normalizedSource{URL: trimmed, Platform: domain.PlatformBilibili}, nil
	}

	return normalizedSource{}, errors.New(unsupportedURLMessage)
}

func normalizeYouTubeURL(raw string) (string, error) {
	source, err := normalizeSourceURL(raw)
	if err != nil {
		return "", err
	}
	if source.Platform != domain.PlatformYouTube {
		return "", errors.New(unsupportedURLMessage)
	}
	return source.URL, nil
}
