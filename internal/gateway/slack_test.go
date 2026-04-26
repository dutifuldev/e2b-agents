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
}

func TestIsBotMentionText(t *testing.T) {
	if !isBotMentionText("hello <@U123>", "U123") {
		t.Fatal("expected bot mention to match")
	}
	if isBotMentionText("hello <@U456>", "U123") {
		t.Fatal("expected other user mention not to match")
	}
}
