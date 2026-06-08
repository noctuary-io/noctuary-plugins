package gojson_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/gojson"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func match(t *testing.T, body string) (*schema.ContextEvent, error) {
	t.Helper()
	return gojson.New().Match(schema.ParsedOTelLog{
		ServiceName: "checkout-service",
		Body:        body,
	})
}

func TestPlugin_Vendor(t *testing.T) {
	if gojson.New().Vendor() != "gojson" {
		t.Error("Vendor() should be gojson")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	if len(gojson.New().Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func TestPlugin_Match_ZapError(t *testing.T) {
	body := `{"level":"error","ts":1705314221.123,"caller":"checkout/service.go:42","msg":"failed to place order","error":"connection refused"}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.EventType != schema.EventTypeDependencyFailure {
		t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
	}
	if ev.NewValue != "connection refused" {
		t.Errorf("NewValue = %q, want the error field value", ev.NewValue)
	}
}

func TestPlugin_Match_SlogError(t *testing.T) {
	body := `{"time":"2024-01-15T10:23:41.123Z","level":"ERROR","source":{"function":"main.run","file":"main.go","line":55},"msg":"request failed","error":"upstream timeout"}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.EventType != schema.EventTypeDependencyFailure {
		t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
	}
}

func TestPlugin_Match_PaymentChargeFailure(t *testing.T) {
	body := `{"level":"error","ts":1705314221.456,"caller":"payment/charge.go:88","msg":"failed to charge","error":"card declined: insufficient funds"}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeDependencyFailure {
		t.Errorf("EventType = %q, want dependency_failure", ev.EventType)
	}
	if ev.Confidence < 0.9 {
		t.Errorf("Confidence = %.2f, want >= 0.9 for payment charge failure", ev.Confidence)
	}
}

func TestPlugin_Match_Panic(t *testing.T) {
	body := `{"level":"fatal","ts":1705314221.789,"caller":"main.go:10","msg":"runtime panic","stacktrace":"goroutine 1 [running]:\nmain.main()\n\t/app/main.go:10 +0x1a"}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeRestart {
		t.Errorf("EventType = %q, want restart (panic is a process exit)", ev.EventType)
	}
}

func TestPlugin_Match_Noise(t *testing.T) {
	noiseLines := []struct {
		name string
		body string
	}{
		{
			name: "info level is ignored",
			body: `{"level":"info","ts":1705314221.123,"msg":"server started","port":8080}`,
		},
		{
			name: "warn level is ignored",
			body: `{"level":"warn","ts":1705314221.456,"msg":"slow query detected","duration":"2s"}`,
		},
		{
			name: "error without error field",
			body: `{"level":"error","ts":1705314221.789,"msg":"something went wrong"}`,
		},
		{
			name: "non-JSON line",
			body: `2024-01-15T10:23:41Z ERROR failed to connect`,
		},
	}
	for _, tt := range noiseLines {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev != nil {
				t.Errorf("expected nil for noise line, got event: EventType=%q body: %s", ev.EventType, tt.body)
			}
		})
	}
}
