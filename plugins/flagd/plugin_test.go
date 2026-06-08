package flagd_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/flagd"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func match(t *testing.T, body string) (*schema.ContextEvent, error) {
	t.Helper()
	return flagd.New().Match(schema.ParsedOTelLog{
		ServiceName: "flagd",
		Body:        body,
	})
}

func TestPlugin_Vendor(t *testing.T) {
	if flagd.New().Vendor() != "flagd" {
		t.Error("Vendor() should be flagd")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	if len(flagd.New().Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func TestPlugin_Match_FlagFlip(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantEntity  string
		wantOldVal  string
		wantNewVal  string
		wantNil     bool
	}{
		{
			name:       "flag flipped to non-default — fire event",
			body:       `{"level":"info","ts":1705314221.123,"msg":"flag evaluation","flagKey":"paymentServiceFailure","variant":"on","defaultVariant":"off"}`,
			wantEntity: "paymentServiceFailure",
			wantOldVal: "off",
			wantNewVal: "on",
		},
		{
			name:       "flag returning default — suppress (noise)",
			body:       `{"level":"info","ts":1705314221.456,"msg":"flag evaluation","flagKey":"paymentServiceFailure","variant":"off","defaultVariant":"off"}`,
			wantNil:    true,
		},
		{
			name:       "another flag flipped",
			body:       `{"level":"info","ts":1705314222.000,"msg":"flag evaluation","flagKey":"kafkaQueueProblems","variant":"problemSimulation","defaultVariant":"off"}`,
			wantEntity: "kafkaQueueProblems",
			wantOldVal: "off",
			wantNewVal: "problemSimulation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil (default-variant suppressed), got event: %+v", ev)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeFlagChange {
				t.Errorf("EventType = %q, want flag_change", ev.EventType)
			}
			if ev.Entity != tt.wantEntity {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantEntity)
			}
			if ev.OldValue != tt.wantOldVal {
				t.Errorf("OldValue = %q, want %q", ev.OldValue, tt.wantOldVal)
			}
			if ev.NewValue != tt.wantNewVal {
				t.Errorf("NewValue = %q, want %q", ev.NewValue, tt.wantNewVal)
			}
			if ev.Confidence < 0.95 {
				t.Errorf("Confidence = %.2f, want >= 0.95 (flag flip is definitive)", ev.Confidence)
			}
		})
	}
}

func TestPlugin_Match_Noise(t *testing.T) {
	noiseLines := []struct {
		name string
		body string
	}{
		{
			name: "non-JSON line",
			body: `2024-01-15 INFO starting flagd service`,
		},
		{
			name: "JSON without flagKey",
			body: `{"level":"info","ts":1705314221.123,"msg":"server started","port":8013}`,
		},
		{
			name: "valid evaluation at default variant",
			body: `{"level":"info","ts":1705314221.456,"msg":"flag evaluation","flagKey":"cartServiceFailure","variant":"off","defaultVariant":"off"}`,
		},
	}
	for _, tt := range noiseLines {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev != nil {
				t.Errorf("expected nil for noise line, got event: %+v", ev)
			}
		})
	}
}

func TestPlugin_Match_FlagFlip_OTelAttributes(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		attrs      map[string]string
		wantNil    bool
		wantEntity string
		wantOld    string
		wantNew    string
	}{
		{
			name:       "flag flipped via OTel attributes",
			body:       "flag evaluation",
			attrs:      map[string]string{"flagKey": "paymentServiceFailure", "variant": "on", "defaultVariant": "off"},
			wantEntity: "paymentServiceFailure",
			wantOld:    "off",
			wantNew:    "on",
		},
		{
			name:    "default variant via OTel attributes is suppressed",
			body:    "flag evaluation",
			attrs:   map[string]string{"flagKey": "paymentServiceFailure", "variant": "off", "defaultVariant": "off"},
			wantNil: true,
		},
		{
			name:       "chaos flag triggered via attributes",
			body:       "flag evaluation",
			attrs:      map[string]string{"flagKey": "kafkaQueueProblems", "variant": "problemSimulation", "defaultVariant": "off"},
			wantEntity: "kafkaQueueProblems",
			wantOld:    "off",
			wantNew:    "problemSimulation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := flagd.New().Match(schema.ParsedOTelLog{
				ServiceName: "flagd",
				Body:        tt.body,
				Attributes:  tt.attrs,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil (suppressed), got %+v", ev)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeFlagChange {
				t.Errorf("EventType = %q, want flag_change", ev.EventType)
			}
			if ev.Entity != tt.wantEntity {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantEntity)
			}
			if ev.OldValue != tt.wantOld {
				t.Errorf("OldValue = %q, want %q", ev.OldValue, tt.wantOld)
			}
			if ev.NewValue != tt.wantNew {
				t.Errorf("NewValue = %q, want %q", ev.NewValue, tt.wantNew)
			}
		})
	}
}

func TestPlugin_Match_FlagFlip_RequiredFields(t *testing.T) {
	body := `{"level":"info","ts":1705314221.123,"msg":"flag evaluation","flagKey":"paymentServiceFailure","variant":"on","defaultVariant":"off"}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Vendor != "flagd" {
		t.Errorf("Vendor = %q, want flagd", ev.Vendor)
	}
	if ev.TTLSeconds <= 0 {
		t.Errorf("TTLSeconds = %d, must be > 0", ev.TTLSeconds)
	}
	if ev.RawLine == "" {
		t.Error("RawLine must not be empty")
	}
}
