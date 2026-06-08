package pino_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/pino"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func match(t *testing.T, body string) (*schema.ContextEvent, error) {
	t.Helper()
	return pino.New().Match(schema.ParsedOTelLog{
		ServiceName: "payment-service",
		Body:        body,
	})
}

func TestPlugin_Vendor(t *testing.T) {
	if pino.New().Vendor() != "pino" {
		t.Error("Vendor() should be pino")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	if len(pino.New().Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func TestPlugin_Match_ErrorLevel(t *testing.T) {
	body := `{"level":50,"time":1705314221123,"msg":"payment processing failed","pid":1,"err":{"message":"connection refused","type":"Error","stack":"Error: connection refused\n    at Socket.<anonymous>"}}`
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
	if ev.NewValue != "Error: connection refused" {
		t.Errorf("NewValue = %q, want err.type: err.message", ev.NewValue)
	}
}

func TestPlugin_Match_FatalLevel(t *testing.T) {
	body := `{"level":60,"time":1705314221456,"msg":"unhandled exception — process exiting","pid":1}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeRestart {
		t.Errorf("EventType = %q, want restart (level 60 is fatal)", ev.EventType)
	}
}

func TestPlugin_Match_ErrorWithoutErrField(t *testing.T) {
	body := `{"level":50,"time":1705314221789,"msg":"database query timeout","pid":1}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.NewValue != "database query timeout" {
		t.Errorf("NewValue = %q, want msg field as fallback", ev.NewValue)
	}
}

func TestPlugin_Match_Noise(t *testing.T) {
	noiseLines := []struct {
		name string
		body string
	}{
		{
			name: "info level (30) is ignored",
			body: `{"level":30,"time":1705314221123,"msg":"server listening","pid":1,"port":8080}`,
		},
		{
			name: "warn level (40) is ignored",
			body: `{"level":40,"time":1705314221456,"msg":"slow response","pid":1,"duration":2000}`,
		},
		{
			name: "debug level (20) is ignored",
			body: `{"level":20,"time":1705314221789,"msg":"processing request","pid":1}`,
		},
		{
			name: "non-JSON line",
			body: `Error: connection refused at payment-service:8080`,
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

func TestPlugin_Match_RequiredFields(t *testing.T) {
	body := `{"level":50,"time":1705314221123,"msg":"request failed","pid":1,"err":{"message":"network error"}}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Vendor != "pino" {
		t.Errorf("Vendor = %q, want pino", ev.Vendor)
	}
	if ev.Confidence <= 0 || ev.Confidence > 1 {
		t.Errorf("Confidence = %.2f out of (0,1]", ev.Confidence)
	}
	if ev.TTLSeconds <= 0 {
		t.Errorf("TTLSeconds = %d, must be > 0", ev.TTLSeconds)
	}
	if ev.RawLine == "" {
		t.Error("RawLine must not be empty")
	}
}
