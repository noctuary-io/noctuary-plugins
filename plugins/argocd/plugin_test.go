package argocd_test

import (
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/argocd"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func TestPlugin_Vendor(t *testing.T) {
	p := argocd.New()
	if p.Vendor() != "argocd" {
		t.Errorf("Vendor() = %q, want %q", p.Vendor(), "argocd")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	p := argocd.New()
	if len(p.Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func match(t *testing.T, body string) (*schema.ContextEvent, error) {
	t.Helper()
	return argocd.New().Match(schema.ParsedOTelLog{
		ServiceName: "argocd-application-controller",
		Body:        body,
	})
}

func TestPlugin_Match_SyncStarting(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantEntity   string
		wantNewValue string
		wantSHA      string
	}{
		{
			name:         "basic sync starting extracts app and SHA",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=payment-service dest-server=https://kubernetes.default.svc revision=a1b2c3d4 namespace=production`,
			wantEntity:   "payment-service",
			wantNewValue: "syncing:a1b2c3d4",
			wantSHA:      "a1b2c3d4",
		},
		{
			name:         "hyphenated app name preserved",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=my-long-service-name dest-server=https://kubernetes.default.svc revision=deadbeef`,
			wantEntity:   "my-long-service-name",
			wantNewValue: "syncing:deadbeef",
			wantSHA:      "deadbeef",
		},
		{
			name:         "quoted app name is parsed correctly",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app="checkout-service" dest-server=https://kubernetes.default.svc revision=f7e3a1b2`,
			wantEntity:   "checkout-service",
			wantNewValue: "syncing:f7e3a1b2",
			wantSHA:      "f7e3a1b2",
		},
		{
			name:         "full 40-char SHA preserved",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=svc dest-server=https://k8s.local revision=a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2`,
			wantEntity:   "svc",
			wantNewValue: "syncing:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			wantSHA:      "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		},
		{
			name:         "sync starting without revision — SHA empty, NewValue is syncing:",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=svc dest-server=https://k8s.local`,
			wantEntity:   "svc",
			wantNewValue: "syncing:",
			wantSHA:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeDeploy {
				t.Errorf("EventType = %q, want deploy", ev.EventType)
			}
			if ev.Entity != tt.wantEntity {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantEntity)
			}
			if ev.NewValue != tt.wantNewValue {
				t.Errorf("NewValue = %q, want %q", ev.NewValue, tt.wantNewValue)
			}
			if ev.SHA != tt.wantSHA {
				t.Errorf("SHA = %q, want %q", ev.SHA, tt.wantSHA)
			}
			if ev.TTLSeconds != 1800 {
				t.Errorf("TTLSeconds = %d, want 1800", ev.TTLSeconds)
			}
		})
	}
}

func TestPlugin_Match_SyncSucceeded(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantEntity   string
		wantNewValue string
		wantSHA      string
	}{
		{
			name:         "basic sync succeeded",
			body:         `time="2024-01-15T10:23:55Z" level=info msg="Sync operation succeeded" app=payment-service revision=a1b2c3d4`,
			wantEntity:   "payment-service",
			wantNewValue: "succeeded",
			wantSHA:      "a1b2c3d4",
		},
		{
			name:         "sync succeeded preserves SHA for post-deploy correlation",
			body:         `time="2024-01-15T10:23:55Z" level=info msg="Sync operation succeeded" app=checkout-service revision=f7e3a1b2`,
			wantEntity:   "checkout-service",
			wantNewValue: "succeeded",
			wantSHA:      "f7e3a1b2",
		},
		{
			name:         "sync succeeded without revision — SHA empty but event still captured",
			body:         `time="2024-01-15T10:23:55Z" level=info msg="Sync operation succeeded" app=svc`,
			wantEntity:   "svc",
			wantNewValue: "succeeded",
			wantSHA:      "",
		},
		{
			name:         "level=warning sync succeeded (unusual but valid)",
			body:         `time="2024-01-15T10:23:55Z" level=warning msg="Sync operation succeeded" app=svc revision=abc1234`,
			wantEntity:   "svc",
			wantNewValue: "succeeded",
			wantSHA:      "abc1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeDeploy {
				t.Errorf("EventType = %q, want deploy", ev.EventType)
			}
			if ev.Entity != tt.wantEntity {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantEntity)
			}
			if ev.NewValue != tt.wantNewValue {
				t.Errorf("NewValue = %q, want %q", ev.NewValue, tt.wantNewValue)
			}
			if ev.SHA != tt.wantSHA {
				t.Errorf("SHA = %q, want %q", ev.SHA, tt.wantSHA)
			}
			if ev.TTLSeconds != 1800 {
				t.Errorf("TTLSeconds = %d, want 1800", ev.TTLSeconds)
			}
			if ev.Confidence < 0.95 {
				t.Errorf("Confidence = %.2f, want >= 0.95 (succeeded is a definitive state)", ev.Confidence)
			}
		})
	}
}

func TestPlugin_Match_SyncFailed(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantEntity string
		wantSHA    string
	}{
		{
			name:       "basic sync failed",
			body:       `time="2024-01-15T10:23:47Z" level=warning msg="Sync operation failed" app=payment-service revision=a1b2c3d4 error="failed to create: admission webhook denied"`,
			wantEntity: "payment-service",
			wantSHA:    "a1b2c3d4",
		},
		{
			name:       "sync failed with long error message — error field is extra context, NewValue is still 'failed'",
			body:       `time="2024-01-15T10:23:47Z" level=error msg="Sync operation failed" app=inventory-service revision=d4c3b2a1 error="ComparisonError: failed to apply resource: the server returned a non-zero exit code (1)"`,
			wantEntity: "inventory-service",
			wantSHA:    "d4c3b2a1",
		},
		{
			name:       "sync failed without revision",
			body:       `time="2024-01-15T10:23:47Z" level=warning msg="Sync operation failed" app=svc error="context deadline exceeded"`,
			wantEntity: "svc",
			wantSHA:    "",
		},
		{
			name:       "sync failed level=error (ArgoCD sometimes uses error level for hard failures)",
			body:       `time="2024-01-15T10:23:47Z" level=error msg="Sync operation failed" app=api-gateway revision=abc1234 error="failed to apply: forbidden"`,
			wantEntity: "api-gateway",
			wantSHA:    "abc1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeDeploy {
				t.Errorf("EventType = %q, want deploy", ev.EventType)
			}
			if ev.NewValue != "failed" {
				t.Errorf("NewValue = %q, want failed", ev.NewValue)
			}
			if ev.Entity != tt.wantEntity {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantEntity)
			}
			if ev.SHA != tt.wantSHA {
				t.Errorf("SHA = %q, want %q", ev.SHA, tt.wantSHA)
			}
		})
	}
}

func TestPlugin_Match_HealthChanged(t *testing.T) {
	// All health state transitions ArgoCD can emit.
	// ArgoCD health states: Healthy, Progressing, Degraded, Suspended, Missing, Unknown.
	tests := []struct {
		name         string
		body         string
		wantOldValue string
		wantNewValue string
	}{
		{
			name:         "Healthy to Degraded — the primary post-deploy regression signal",
			body:         `time="2024-01-15T10:24:30Z" level=warning msg="App health changed" app=checkout-service from=Healthy to=Degraded`,
			wantOldValue: "Healthy",
			wantNewValue: "health:Degraded",
		},
		{
			name:         "Degraded to Healthy — incident resolved, recovery signal",
			body:         `time="2024-01-15T10:25:00Z" level=info msg="App health changed" app=checkout-service from=Degraded to=Healthy`,
			wantOldValue: "Degraded",
			wantNewValue: "health:Healthy",
		},
		{
			name:         "Unknown to Progressing — deploy just kicked off, resources being created",
			body:         `time="2024-01-15T10:23:42Z" level=info msg="App health changed" app=new-service from=Unknown to=Progressing`,
			wantOldValue: "Unknown",
			wantNewValue: "health:Progressing",
		},
		{
			name:         "Progressing to Healthy — rolling deploy completed successfully",
			body:         `time="2024-01-15T10:24:00Z" level=info msg="App health changed" app=api-service from=Progressing to=Healthy`,
			wantOldValue: "Progressing",
			wantNewValue: "health:Healthy",
		},
		{
			name:         "Progressing to Degraded — rolling deploy failed partway through",
			body:         `time="2024-01-15T10:24:15Z" level=warning msg="App health changed" app=api-service from=Progressing to=Degraded`,
			wantOldValue: "Progressing",
			wantNewValue: "health:Degraded",
		},
		{
			name:         "Healthy to Missing — resource was deleted from the cluster",
			body:         `time="2024-01-15T10:24:30Z" level=warning msg="App health changed" app=legacy-service from=Healthy to=Missing`,
			wantOldValue: "Healthy",
			wantNewValue: "health:Missing",
		},
		{
			name:         "Healthy to Suspended — app manually suspended via ArgoCD UI or CLI",
			body:         `time="2024-01-15T10:24:30Z" level=info msg="App health changed" app=batch-job from=Healthy to=Suspended`,
			wantOldValue: "Healthy",
			wantNewValue: "health:Suspended",
		},
		{
			name:         "Degraded to Missing — resources deleted while service was already broken",
			body:         `time="2024-01-15T10:24:30Z" level=warning msg="App health changed" app=svc from=Degraded to=Missing`,
			wantOldValue: "Degraded",
			wantNewValue: "health:Missing",
		},
		{
			name:         "Missing to Progressing — resources being recreated",
			body:         `time="2024-01-15T10:25:00Z" level=info msg="App health changed" app=svc from=Missing to=Progressing`,
			wantOldValue: "Missing",
			wantNewValue: "health:Progressing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeDeploy {
				t.Errorf("EventType = %q, want deploy", ev.EventType)
			}
			if ev.OldValue != tt.wantOldValue {
				t.Errorf("OldValue = %q, want %q (before-state is needed for LLM to understand direction of change)", ev.OldValue, tt.wantOldValue)
			}
			if ev.NewValue != tt.wantNewValue {
				t.Errorf("NewValue = %q, want %q", ev.NewValue, tt.wantNewValue)
			}
			if ev.TTLSeconds != 900 {
				t.Errorf("TTLSeconds = %d, want 900 (health events are shorter-lived context than deploy SHAs)", ev.TTLSeconds)
			}
		})
	}
}

func TestPlugin_Match_AutoSync(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantEntity   string
		wantNewValue string
		wantSHA      string
	}{
		{
			name:         "auto-sync trigger from image updater",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Triggering automatic sync" app=checkout-service revision=f7e3a1b2`,
			wantEntity:   "checkout-service",
			wantNewValue: "auto_syncing:f7e3a1b2",
			wantSHA:      "f7e3a1b2",
		},
		{
			name:         "auto-sync with dest-server present",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Triggering automatic sync" app=payment-service dest-server=https://kubernetes.default.svc revision=abc1234`,
			wantEntity:   "payment-service",
			wantNewValue: "auto_syncing:abc1234",
			wantSHA:      "abc1234",
		},
		{
			name:         "auto-sync without revision — auto_syncing: with empty SHA",
			body:         `time="2024-01-15T10:23:41Z" level=info msg="Triggering automatic sync" app=svc`,
			wantEntity:   "svc",
			wantNewValue: "auto_syncing:",
			wantSHA:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeDeploy {
				t.Errorf("EventType = %q, want deploy: auto-sync is a deploy event", ev.EventType)
			}
			if ev.Entity != tt.wantEntity {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantEntity)
			}
			if ev.NewValue != tt.wantNewValue {
				t.Errorf("NewValue = %q, want %q (auto_syncing prefix distinguishes automated from manual)", ev.NewValue, tt.wantNewValue)
			}
			if ev.SHA != tt.wantSHA {
				t.Errorf("SHA = %q, want %q", ev.SHA, tt.wantSHA)
			}
		})
	}
}

func TestPlugin_Match_RepoFailure(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantEntity string
	}{
		{
			name:       "repo server unavailable — all deploys stalled",
			body:       `time="2024-01-15T10:23:41Z" level=error msg="Failed to get git client for repo" app=checkout-service error="rpc error: code = Unavailable desc = connection refused"`,
			wantEntity: "checkout-service",
		},
		{
			name:       "repo failure during active reconciliation",
			body:       `time="2024-01-15T10:23:41Z" level=error msg="Failed to get git client for repo" app=payment-service error="repo server unavailable: dial tcp: connection timed out"`,
			wantEntity: "payment-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != schema.EventTypeDependencyFailure {
				t.Errorf("EventType = %q, want dependency_failure: repo unavailability is an infra dependency, not a deploy state", ev.EventType)
			}
			if ev.NewValue != "repo_unavailable" {
				t.Errorf("NewValue = %q, want repo_unavailable", ev.NewValue)
			}
			if ev.Entity != tt.wantEntity {
				t.Errorf("Entity = %q, want %q", ev.Entity, tt.wantEntity)
			}
			if ev.TTLSeconds != 1800 {
				t.Errorf("TTLSeconds = %d, want 1800: repo failures can affect many subsequent deploys", ev.TTLSeconds)
			}
		})
	}
}

func TestPlugin_Match_Noise(t *testing.T) {
	// Every one of these lines must return nil — they carry no diagnostic signal.
	noiseLines := []struct {
		name string
		body string
	}{
		// Missing app= field — the plugin gate that prevents all false positives.
		{
			name: "debug: Watching cluster — no app field",
			body: `time="2024-01-15T10:23:41Z" level=debug msg="Watching cluster" server=https://kubernetes.default.svc`,
		},
		{
			name: "info: Starting cache — no app field",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Starting cache" resource=Application`,
		},
		{
			name: "info: database system is ready — completely different service, no app field",
			body: `2024-01-15 10:23:41.000 UTC [1] LOG:  database system is ready to accept connections`,
		},

		// Lines that have app= but are operational noise with no recognised message.
		{
			name: "info: Reconciliation completed successfully — operational heartbeat",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Reconciliation of app completed successfully" app=checkout-service`,
		},
		{
			name: "info: Comparing app state — sync check, fires every few seconds",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Comparing app state" app=checkout-service`,
		},
		{
			name: "info: Normalizing app resources — resource normalisation during sync",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Normalizing app resources" app=checkout-service`,
		},
		{
			name: "info: Updating app — generic status update, not a sync lifecycle event",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Updating app" app=checkout-service`,
		},
		{
			name: "info: App unchanged — no diff detected, nothing deployed",
			body: `time="2024-01-15T10:23:41Z" level=info msg="App unchanged, skipping reconciliation" app=checkout-service`,
		},
		{
			name: "info: Skipping latest committed version — auto-sync suppressed",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Skipping latest committed version" app=checkout-service revision=abc1234`,
		},
		{
			name: "info: Processing app — reconcile loop tick",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Processing app" app=checkout-service`,
		},
		{
			name: "info: Refresh app — forced refresh, not a deploy",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Refreshing app" app=checkout-service`,
		},
		{
			name: "debug: Enqueued app for reconciliation — internal queue noise",
			body: `time="2024-01-15T10:23:41Z" level=debug msg="Enqueued app for reconciliation" app=checkout-service`,
		},
		{
			name: "info: argocd-server log — different component, not app controller",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Request served" method=GET path=/api/v1/applications status=200`,
		},
		{
			name: "info: repo-server log — not application-controller",
			body: `time="2024-01-15T10:23:41Z" level=info msg="Git fetch" repo=https://github.com/myorg/myrepo`,
		},
		{
			name: "warning: with app field but unrecognised message — should not match",
			body: `time="2024-01-15T10:23:41Z" level=warning msg="App is out of sync" app=checkout-service`,
		},
	}

	for _, tt := range noiseLines {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := match(t, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev != nil {
				t.Errorf("expected nil for noise line, got event: EventType=%q NewValue=%q\nbody: %s",
					ev.EventType, ev.NewValue, tt.body)
			}
		})
	}
}

func TestPlugin_Match_DoesNotFireOnPostgresLogs(t *testing.T) {
	pgLines := []string{
		`2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 4521.234 ms  statement: SELECT * FROM orders`,
		`2024-01-15 10:23:41.456 UTC [1235] postgres@appdb LOG:  automatic vacuum of table "appdb.public.orders"`,
		`2024-01-15 10:23:41.789 UTC [1236] appuser@appdb ERROR:  deadlock detected`,
		`2024-01-15 10:23:42.000 UTC [1237] FATAL:  remaining connection slots are reserved`,
		`2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint starting: time`,
	}

	for _, body := range pgLines {
		ev, err := match(t, body)
		if err != nil {
			t.Errorf("unexpected error for line %q: %v", body, err)
		}
		if ev != nil {
			t.Errorf("ArgoCD plugin fired on Postgres line: %q", body)
		}
	}
}

func TestPlugin_Match_DoesNotFireOnKubernetesEvents(t *testing.T) {
	import_marker := `{"kind":"Event","reason":"OOMKilling","message":"Memory limit reached","type":"Warning","count":1,"lastTimestamp":"2024-01-15T10:23:41Z","involvedObject":{"kind":"Pod","namespace":"production","name":"svc-abc"}}`
	ev, err := match(t, import_marker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev != nil {
		t.Errorf("ArgoCD plugin fired on Kubernetes event JSON")
	}
}

func TestPlugin_Match_AllEventsHaveRequiredFields(t *testing.T) {
	// Verify the schema contract for every event type the ArgoCD plugin produces.
	representativeLines := []string{
		`time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=svc dest-server=https://k8s.local revision=abc1234`,
		`time="2024-01-15T10:23:55Z" level=info msg="Sync operation succeeded" app=svc revision=abc1234`,
		`time="2024-01-15T10:23:47Z" level=warning msg="Sync operation failed" app=svc revision=abc1234`,
		`time="2024-01-15T10:24:30Z" level=warning msg="App health changed" app=svc from=Healthy to=Degraded`,
		`time="2024-01-15T10:23:41Z" level=info msg="Triggering automatic sync" app=svc revision=abc1234`,
		`time="2024-01-15T10:23:41Z" level=error msg="Failed to get git client for repo" app=svc error="unavailable"`,
	}

	for _, body := range representativeLines {
		ev, err := match(t, body)
		if err != nil {
			t.Fatalf("error for %q: %v", body, err)
		}
		if ev == nil {
			t.Fatalf("nil event for %q", body)
		}
		if ev.Vendor != "argocd" {
			t.Errorf("Vendor = %q, want argocd", ev.Vendor)
		}
		if ev.EventType == "" {
			t.Errorf("EventType empty for %q", body)
		}
		if ev.Entity == "" {
			t.Errorf("Entity empty for %q", body)
		}
		if ev.Timestamp == "" {
			t.Errorf("Timestamp empty for %q", body)
		}
		if ev.Confidence <= 0 || ev.Confidence > 1 {
			t.Errorf("Confidence = %.2f out of (0,1] for %q", ev.Confidence, body)
		}
		if ev.TTLSeconds <= 0 {
			t.Errorf("TTLSeconds = %d, must be > 0 for %q", ev.TTLSeconds, body)
		}
		if ev.RawLine == "" {
			t.Errorf("RawLine empty for %q", body)
		}
	}
}
