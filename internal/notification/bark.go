package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type BarkClient struct {
	httpClient *http.Client
}

type BarkMessage struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Group     string `json:"group,omitempty"`
}

func NewBarkClient(client *http.Client) *BarkClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &BarkClient{httpClient: client}
}

func (c *BarkClient) Send(ctx context.Context, serverURL string, message BarkMessage) error {
	endpoint, err := barkPushURL(serverURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(message.DeviceKey) == "" {
		return errors.New("Bark device key 为空")
	}
	if strings.TrimSpace(message.Body) == "" {
		return errors.New("Bark 消息内容为空")
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		if len(body) > 0 {
			return fmt.Errorf("Bark 返回 %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("Bark 返回 %d", res.StatusCode)
	}
	return nil
}

func barkPushURL(serverURL string) (string, error) {
	trimmed := strings.TrimSpace(serverURL)
	if trimmed == "" {
		trimmed = "https://api.day.app"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("Bark 服务端地址无效")
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if basePath == "/push" {
		parsed.Path = basePath
	} else {
		parsed.Path = basePath + "/push"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
