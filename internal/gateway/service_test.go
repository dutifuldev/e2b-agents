package gateway

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dutifuldev/e2b-agents/internal/database"
)

func TestHandleSlackEnvelopeDedupesConcurrentRetry(t *testing.T) {
	tmp := t.TempDir()
	countPath := filepath.Join(tmp, "send-count")
	scriptPath := filepath.Join(tmp, "runtime.sh")
	script := `#!/bin/sh
payload=$(cat)
case "$payload" in
*'"command":"ensure"'*)
  printf '{"sandboxId":"sandbox-1","templateId":"openclaw","host":"localhost","baseUrl":"http://localhost","sessionKey":"session-1"}'
  ;;
*'"command":"send"'*)
  sleep 0.1
  printf x >> "` + countPath + `"
  printf '{"text":"ok","sessionKey":"session-1"}'
  ;;
*)
  printf 'unknown command' >&2
  exit 1
  ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write runtime script: %v", err)
	}

	db, err := database.Open(":memory:", database.PoolConfig{MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.ApplyTestSchema(db); err != nil {
		t.Fatalf("apply test schema: %v", err)
	}

	service := NewService(db, Options{
		Runtime: NewRuntimeClient(RuntimeOptions{
			NodePath:     "/bin/sh",
			ScriptPath:   scriptPath,
			APIKey:       "test-e2b-key",
			AnthropicKey: "test-anthropic-key",
			Timeout:      time.Minute,
		}),
		AutoCreate:      true,
		DefaultTeamID:   "default",
		DefaultTemplate: "openclaw",
	})

	envelope := SlackEventEnvelope{
		TeamID:  "T123",
		EventID: "Ev123",
		Event: SlackEvent{
			Type:        "app_mention",
			ChannelType: "channel",
			Channel:     "",
			User:        "U123",
			Text:        "<@BOT> hello",
			TS:          "1777220000.000100",
		},
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if err := service.handleSlackEnvelope(context.Background(), envelope); err != nil {
				t.Errorf("handleSlackEnvelope() returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	count, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatalf("read send count: %v", err)
	}
	if got := string(count); got != "x" {
		t.Fatalf("runtime send count = %q, want one send", got)
	}
}
