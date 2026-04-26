package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type SlackClient struct {
	token      string
	httpClient *http.Client
}

type SlackAuthInfo struct {
	TeamID string `json:"team_id"`
	Team   string `json:"team"`
	UserID string `json:"user_id"`
	BotID  string `json:"bot_id"`
}

func NewSlackClient(token string) *SlackClient {
	return &SlackClient{
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func SlackTokenFromRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("slack bot token reference is empty")
	}
	name, ok := strings.CutPrefix(ref, "env:")
	if !ok {
		return "", fmt.Errorf("unsupported slack token reference: %s", ref)
	}
	token := strings.TrimSpace(os.Getenv(name))
	if token == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	return token, nil
}

func VerifySlackSignature(signingSecret string, headers http.Header, body []byte, now time.Time) error {
	signingSecret = strings.TrimSpace(signingSecret)
	if signingSecret == "" {
		return errors.New("slack signing secret is not configured")
	}
	timestamp := strings.TrimSpace(headers.Get("X-Slack-Request-Timestamp"))
	signature := strings.TrimSpace(headers.Get("X-Slack-Signature"))
	if timestamp == "" || signature == "" {
		return errors.New("missing slack signature headers")
	}
	parsed, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("invalid slack timestamp")
	}
	requestTime := time.Unix(parsed, 0)
	if now.Sub(requestTime) > 5*time.Minute || requestTime.Sub(now) > 5*time.Minute {
		return errors.New("stale slack timestamp")
	}

	base := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte(base))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return errors.New("invalid slack signature")
	}
	return nil
}

func (c *SlackClient) AuthTest(ctx context.Context) (SlackAuthInfo, error) {
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		SlackAuthInfo
	}
	if err := c.apiJSON(ctx, "auth.test", nil, &response); err != nil {
		return SlackAuthInfo{}, err
	}
	if !response.OK {
		return SlackAuthInfo{}, fmt.Errorf("slack auth.test failed: %s", response.Error)
	}
	return response.SlackAuthInfo, nil
}

func (c *SlackClient) PostMessage(ctx context.Context, channel, threadTS, text string) error {
	payload := map[string]any{
		"channel": channel,
		"text":    NormalizeSlackText(text),
	}
	if strings.TrimSpace(threadTS) != "" {
		payload["thread_ts"] = strings.TrimSpace(threadTS)
	}
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.apiJSON(ctx, "chat.postMessage", payload, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("slack chat.postMessage failed: %s", response.Error)
	}
	return nil
}

func (c *SlackClient) apiJSON(ctx context.Context, method string, payload any, out any) error {
	if c.token == "" {
		return errors.New("slack bot token is not configured")
	}
	var body io.Reader
	contentType := "application/x-www-form-urlencoded"
	if payload == nil {
		body = strings.NewReader(url.Values{}.Encode())
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
		contentType = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/"+method, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack api %s returned HTTP %d", method, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func NormalizeSlackText(text string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

type SlackEventEnvelope struct {
	Token          string          `json:"token"`
	Challenge      string          `json:"challenge"`
	Type           string          `json:"type"`
	TeamID         string          `json:"team_id"`
	EnterpriseID   string          `json:"enterprise_id"`
	EventID        string          `json:"event_id"`
	EventTime      int64           `json:"event_time"`
	Authorizations json.RawMessage `json:"authorizations"`
	Event          SlackEvent      `json:"event"`
}

type SlackEvent struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	User        string `json:"user"`
	BotID       string `json:"bot_id"`
	Text        string `json:"text"`
	Channel     string `json:"channel"`
	TS          string `json:"ts"`
	ThreadTS    string `json:"thread_ts"`
	Team        string `json:"team"`
	EventTS     string `json:"event_ts"`
	ChannelType string `json:"channel_type"`
}
