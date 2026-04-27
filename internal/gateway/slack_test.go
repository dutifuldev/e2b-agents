package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"
)

func TestVerifySlackSignature(t *testing.T) {
	body := []byte(`{"type":"event_callback"}`)
	timestamp := "1777220000"
	secret := "test-secret"
	base := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(base))
	signature := "v0=" + hex.EncodeToString(mac.Sum(nil))

	headers := http.Header{}
	headers.Set("X-Slack-Request-Timestamp", timestamp)
	headers.Set("X-Slack-Signature", signature)

	if err := VerifySlackSignature(secret, headers, body, time.Unix(1777220001, 0)); err != nil {
		t.Fatalf("VerifySlackSignature returned error: %v", err)
	}
}

func TestVerifySlackSignatureRejectsInvalidSignature(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Slack-Request-Timestamp", "1777220000")
	headers.Set("X-Slack-Signature", "v0=bad")

	if err := VerifySlackSignature("test-secret", headers, []byte(`{}`), time.Unix(1777220001, 0)); err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func TestNormalizeSlackText(t *testing.T) {
	got := NormalizeSlackText(" hello  \n\n\n*world*   \r\n")
	want := "hello\n\n*world*"
	if got != want {
		t.Fatalf("NormalizeSlackText() = %q, want %q", got, want)
	}
}

func TestSlackTokenFromRef(t *testing.T) {
	t.Setenv("TEST_SLACK_TOKEN", "xoxb-test")

	got, err := SlackTokenFromRef("env:TEST_SLACK_TOKEN")
	if err != nil {
		t.Fatalf("SlackTokenFromRef() returned error: %v", err)
	}
	if got != "xoxb-test" {
		t.Fatalf("SlackTokenFromRef() = %q, want xoxb-test", got)
	}

	got, err = SlackTokenFromRef("literal:xoxb-oauth")
	if err != nil {
		t.Fatalf("SlackTokenFromRef() literal returned error: %v", err)
	}
	if got != "xoxb-oauth" {
		t.Fatalf("SlackTokenFromRef() literal = %q, want xoxb-oauth", got)
	}
}

func TestIsBotMentionText(t *testing.T) {
	if !isBotMentionText("hello <@U123>", "U123") {
		t.Fatal("expected bot mention to match")
	}
	if isBotMentionText("hello <@U456>", "U123") {
		t.Fatal("expected other user mention not to match")
	}
}

func TestShouldHandleSlackEvent(t *testing.T) {
	cases := []struct {
		name  string
		event SlackEvent
		want  bool
	}{
		{
			name:  "app mention in channel",
			event: SlackEvent{Type: "app_mention", ChannelType: "channel"},
			want:  true,
		},
		{
			name:  "direct message",
			event: SlackEvent{Type: "message", ChannelType: "im"},
			want:  true,
		},
		{
			name:  "ordinary channel message",
			event: SlackEvent{Type: "message", ChannelType: "channel"},
			want:  false,
		},
		{
			name:  "bot message",
			event: SlackEvent{Type: "message", ChannelType: "im", BotID: "B123"},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldHandleSlackEvent(tc.event); got != tc.want {
				t.Fatalf("shouldHandleSlackEvent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSlackSessionKeyScopesConversationSurface(t *testing.T) {
	first := slackSessionKey("T123", "C123", "channel")
	second := slackSessionKey("T123", "C123", "channel")
	if first != second {
		t.Fatalf("slackSessionKey() mismatch: %q != %q", first, second)
	}
	if got := slackSessionKey("T123", "C123", ""); got != "slack-v1-T123-C123-direct" {
		t.Fatalf("slackSessionKey() = %q, want direct fallback", got)
	}
	if got := slackSessionKey("T123", "C123", "1777220000.000000"); got != "slack-v1-T123-C123-1777220000.000000" {
		t.Fatalf("slackSessionKey() = %q, want gateway-safe thread key", got)
	}
	if got := slackSessionKey("T123", "C123", "bad:value"); got != "slack-v1-T123-C123-bad_value" {
		t.Fatalf("slackSessionKey() = %q, want unsafe characters replaced", got)
	}
}

func TestSessionConversationID(t *testing.T) {
	if got := sessionConversationID(SlackEvent{Type: "message", ChannelType: "im", Channel: "D123", TS: "1777220000.000100"}); got != "direct" {
		t.Fatalf("sessionConversationID() = %q, want direct", got)
	}
	if got := sessionConversationID(SlackEvent{Type: "message", Channel: "D123", TS: "1777220000.000100"}); got != "direct" {
		t.Fatalf("sessionConversationID() = %q, want direct fallback", got)
	}
	if got := sessionConversationID(SlackEvent{Type: "app_mention", ChannelType: "channel", Channel: "C123", TS: "1777220000.000100"}); got != "channel" {
		t.Fatalf("sessionConversationID() = %q, want channel", got)
	}
	if got := sessionConversationID(SlackEvent{Type: "app_mention", ChannelType: "channel", Channel: "C123", TS: "1777220000.000100", ThreadTS: " 1777220000.000000 "}); got != "1777220000.000000" {
		t.Fatalf("sessionConversationID() = %q, want thread timestamp", got)
	}
}

func TestReplyThreadTS(t *testing.T) {
	if got := replyThreadTS(SlackEvent{Type: "message", ChannelType: "im", TS: "1777220000.000100"}); got != "" {
		t.Fatalf("replyThreadTS() = %q, want empty direct reply", got)
	}
	if got := replyThreadTS(SlackEvent{Type: "app_mention", TS: "1777220000.000100"}); got != "" {
		t.Fatalf("replyThreadTS() = %q, want top-level app mention to reply in channel", got)
	}
	if got := replyThreadTS(SlackEvent{Type: "app_mention", ChannelType: "channel", TS: "1777220000.000100", ThreadTS: " 1777220000.000000 "}); got != "1777220000.000000" {
		t.Fatalf("replyThreadTS() = %q, want existing thread timestamp", got)
	}
}
