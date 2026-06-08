package log4j2_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/log4j2"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func match(t *testing.T, body string) (*schema.ContextEvent, error) {
	t.Helper()
	return log4j2.New().Match(schema.ParsedOTelLog{
		ServiceName: "ad-service",
		Body:        body,
	})
}

func TestPlugin_Vendor(t *testing.T) {
	if log4j2.New().Vendor() != "log4j2" {
		t.Error("Vendor() should be log4j2")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	if len(log4j2.New().Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func TestPlugin_Match_ThrownException(t *testing.T) {
	body := `{"instant":{"epochSecond":1705314221,"nanoOfSecond":123000000},"thread":"http-exec-1","level":"ERROR","loggerName":"org.springframework.web.servlet.DispatcherServlet","message":"Servlet.service() threw exception","thrown":{"name":"java.net.ConnectException","message":"Connection refused"}}`
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
	if ev.NewValue != "java.net.ConnectException: Connection refused" {
		t.Errorf("NewValue = %q, want exception class: message", ev.NewValue)
	}
	if ev.Confidence < 0.88 {
		t.Errorf("Confidence = %.2f, want >= 0.88", ev.Confidence)
	}
}

func TestPlugin_Match_OOMException(t *testing.T) {
	body := `{"instant":{"epochSecond":1705314221,"nanoOfSecond":0},"thread":"main","level":"ERROR","loggerName":"com.example.AdService","message":"JVM heap exhausted","thrown":{"name":"java.lang.OutOfMemoryError","message":"Java heap space"}}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeRestart {
		t.Errorf("EventType = %q, want restart (OOM is a process death)", ev.EventType)
	}
}

func TestPlugin_Match_FatalWithoutThrown(t *testing.T) {
	body := `{"instant":{"epochSecond":1705314221,"nanoOfSecond":0},"thread":"main","level":"FATAL","loggerName":"com.example.Bootstrap","message":"Application startup failed — database unreachable"}`
	ev, err := match(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != schema.EventTypeRestart {
		t.Errorf("EventType = %q, want restart", ev.EventType)
	}
}

func TestPlugin_Match_ErrorWithoutThrown(t *testing.T) {
	body := `{"instant":{"epochSecond":1705314221,"nanoOfSecond":0},"thread":"scheduler","level":"ERROR","loggerName":"com.example.TaskScheduler","message":"Task execution failed: upstream service unavailable"}`
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
}

func TestPlugin_Match_Noise(t *testing.T) {
	noiseLines := []struct {
		name string
		body string
	}{
		{
			name: "INFO level without exception",
			body: `{"instant":{"epochSecond":1705314221,"nanoOfSecond":0},"thread":"main","level":"INFO","loggerName":"com.example.App","message":"Application started successfully"}`,
		},
		{
			name: "WARN level without thrown",
			body: `{"instant":{"epochSecond":1705314221,"nanoOfSecond":0},"thread":"pool-1","level":"WARN","loggerName":"org.hibernate.engine","message":"HHH90000022: Connections not released after they exceeded timeout"}`,
		},
		{
			name: "non-JSON line",
			body: `[2024-01-15 10:23:41,123] INFO Application started (com.example.App)`,
		},
		{
			name: "empty body",
			body: ``,
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
