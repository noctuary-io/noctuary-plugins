package postgres_test

import (
	"strings"
	"testing"

	"github.com/noctuary-io/noctuary-plugins/plugins/postgres"
	"github.com/noctuary-io/noctuary-plugins/schema"
)

func TestPlugin_Vendor(t *testing.T) {
	p := postgres.New()
	if p.Vendor() != "postgres" {
		t.Errorf("Vendor() = %q, want %q", p.Vendor(), "postgres")
	}
}

func TestPlugin_FingerprintsNonEmpty(t *testing.T) {
	p := postgres.New()
	if len(p.Fingerprints()) == 0 {
		t.Error("Fingerprints() must not be empty")
	}
}

func pg(body string) schema.ParsedOTelLog {
	return schema.ParsedOTelLog{ServiceName: "postgres", Body: body}
}

func mustMatch(t *testing.T, body string) *schema.ContextEvent {
	t.Helper()
	ev, err := postgres.New().Match(pg(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatalf("expected event, got nil for: %s", body)
	}
	return ev
}

func mustNil(t *testing.T, body string) {
	t.Helper()
	ev, err := postgres.New().Match(pg(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev != nil {
		t.Errorf("expected nil for noise line, got EventType=%q NewValue=%q\nbody: %s",
			ev.EventType, ev.NewValue, body)
	}
}

// ---- Slow queries -----------------------------------------------------------

func TestMatch_SlowQuery(t *testing.T) {
	t.Run("basic slow query extracts duration", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 4521.234 ms  statement: SELECT * FROM orders WHERE customer_id = $1`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.NewValue != "slow_query:4521.234ms" {
			t.Errorf("NewValue = %q, want slow_query:4521.234ms", ev.NewValue)
		}
		if ev.Entity != "appdb" {
			t.Errorf("Entity = %q, want appdb", ev.Entity)
		}
		if ev.Actor != "appuser" {
			t.Errorf("Actor = %q, want appuser (user identifies which app pool is slow)", ev.Actor)
		}
		if ev.TTLSeconds != 1200 {
			t.Errorf("TTLSeconds = %d, want 1200", ev.TTLSeconds)
		}
		if ev.Confidence < 0.90 {
			t.Errorf("Confidence = %.2f, want >= 0.90", ev.Confidence)
		}
	})

	t.Run("very slow query — 15 seconds", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 15234.567 ms  statement: SELECT * FROM reports`)
		if ev.NewValue != "slow_query:15234.567ms" {
			t.Errorf("NewValue = %q, want slow_query:15234.567ms", ev.NewValue)
		}
	})

	t.Run("slow query without fractional milliseconds", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 4521 ms  statement: SELECT 1`)
		if ev.NewValue != "slow_query:4521ms" {
			t.Errorf("NewValue = %q, want slow_query:4521ms", ev.NewValue)
		}
	})

	t.Run("slow query without user@db prefix — entity falls back to ServiceName", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] LOG:  duration: 5000.000 ms  statement: SELECT 1`)
		// No user@db in prefix — entity comes from ServiceName ("postgres")
		if ev.Entity == "" {
			t.Error("Entity empty: must fall back to ServiceName when log prefix has no db field")
		}
	})

	t.Run("slow query with complex multi-table JOIN", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.123 UTC [1234] api@appdb LOG:  duration: 8200.000 ms  statement: SELECT o.id, c.name, p.title FROM orders o JOIN customers c ON o.customer_id = c.id JOIN products p ON o.product_id = p.id WHERE o.created_at > $1 ORDER BY o.created_at DESC`)
		if !strings.HasPrefix(ev.NewValue, "slow_query:") {
			t.Errorf("NewValue = %q, want slow_query: prefix", ev.NewValue)
		}
		if ev.Actor != "api" {
			t.Errorf("Actor = %q, want api", ev.Actor)
		}
	})

	t.Run("slow query via prepared statement execute", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  duration: 6100.000 ms  execute stmt1: SELECT id FROM users WHERE email = $1`)
		if !strings.HasPrefix(ev.NewValue, "slow_query:") {
			t.Errorf("NewValue = %q, want slow_query: prefix", ev.NewValue)
		}
	})

	t.Run("slow query different timezone format (EST)", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 05:23:41.123 EST [1234] appuser@appdb LOG:  duration: 4521.234 ms  statement: SELECT 1`)
		if !strings.HasPrefix(ev.NewValue, "slow_query:") {
			t.Errorf("NewValue = %q, want slow_query: prefix", ev.NewValue)
		}
	})
}

// ---- Autovacuum -------------------------------------------------------------

func TestMatch_Autovacuum(t *testing.T) {
	t.Run("autovacuum vacuum extracts fully qualified table name", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.456 UTC [1235] postgres@appdb LOG:  automatic vacuum of table "appdb.public.orders": index scans: 0, pages: 0 removed, 1842 remain`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.Entity != "appdb.public.orders" {
			t.Errorf("Entity = %q, want appdb.public.orders (schema.table identifies hot table)", ev.Entity)
		}
		if ev.NewValue != "autovacuum" {
			t.Errorf("NewValue = %q, want autovacuum", ev.NewValue)
		}
	})

	t.Run("autovacuum on a different table", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.456 UTC [1235] postgres@appdb LOG:  automatic vacuum of table "appdb.public.users": index scans: 1, pages: 12 removed, 500 remain`)
		if ev.Entity != "appdb.public.users" {
			t.Errorf("Entity = %q, want appdb.public.users", ev.Entity)
		}
	})

	t.Run("autovacuum on system catalog", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.456 UTC [1235] postgres@postgres LOG:  automatic vacuum of table "pg_catalog.pg_attribute": index scans: 0, pages: 0 removed, 12 remain`)
		if ev.Entity != "pg_catalog.pg_attribute" {
			t.Errorf("Entity = %q, want pg_catalog.pg_attribute", ev.Entity)
		}
	})

	t.Run("autovacuum analyze — updates table statistics", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.456 UTC [1235] postgres@appdb LOG:  automatic analyze of table "appdb.public.orders": system usage: CPU: user: 0.10 s, system: 0.00 s, elapsed: 1.23 s`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.NewValue != "autovacuum_analyze" {
			t.Errorf("NewValue = %q, want autovacuum_analyze (distinct from vacuum — analyze updates stats, vacuum removes dead tuples)", ev.NewValue)
		}
		if ev.Entity != "appdb.public.orders" {
			t.Errorf("Entity = %q, want appdb.public.orders", ev.Entity)
		}
	})

	t.Run("autovacuum analyze on high-churn payments table", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.456 UTC [1235] postgres@paydb LOG:  automatic analyze of table "paydb.public.transactions": system usage: CPU: user: 2.34 s, elapsed: 5.67 s`)
		if ev.NewValue != "autovacuum_analyze" {
			t.Errorf("NewValue = %q, want autovacuum_analyze", ev.NewValue)
		}
	})
}

// ---- Deadlock ---------------------------------------------------------------

func TestMatch_Deadlock(t *testing.T) {
	t.Run("basic deadlock detected", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.789 UTC [1236] appuser@appdb ERROR:  deadlock detected`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.NewValue != "deadlock_detected" {
			t.Errorf("NewValue = %q, want deadlock_detected", ev.NewValue)
		}
		if ev.Entity != "appdb" {
			t.Errorf("Entity = %q, want appdb", ev.Entity)
		}
		if ev.Actor != "appuser" {
			t.Errorf("Actor = %q, want appuser", ev.Actor)
		}
		if ev.Confidence < 0.93 {
			t.Errorf("Confidence = %.2f, want >= 0.93", ev.Confidence)
		}
	})

	t.Run("deadlock from different user", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.789 UTC [5678] worker@jobdb ERROR:  deadlock detected`)
		if ev.Actor != "worker" {
			t.Errorf("Actor = %q, want worker", ev.Actor)
		}
		if ev.Entity != "jobdb" {
			t.Errorf("Entity = %q, want jobdb", ev.Entity)
		}
	})

	t.Run("deadlock with detail line following", func(t *testing.T) {
		// Postgres logs DETAIL after the ERROR — only the first line is in the log body here.
		ev := mustMatch(t, `2024-01-15 10:23:41.789 UTC [1236] appuser@appdb ERROR:  deadlock detected`)
		if ev.NewValue != "deadlock_detected" {
			t.Errorf("NewValue = %q, want deadlock_detected", ev.NewValue)
		}
	})
}

// ---- Connection exhaustion --------------------------------------------------

func TestMatch_ConnectionExhaustion(t *testing.T) {
	t.Run("connection limit reached — standard message", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:42.000 UTC [1237] FATAL:  remaining connection slots are reserved for non-replication superuser connections`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.NewValue != "connection_limit_reached" {
			t.Errorf("NewValue = %q, want connection_limit_reached", ev.NewValue)
		}
		if ev.Confidence < 0.95 {
			t.Errorf("Confidence = %.2f, want >= 0.95", ev.Confidence)
		}
		if ev.TTLSeconds != 900 {
			t.Errorf("TTLSeconds = %d, want 900", ev.TTLSeconds)
		}
	})

	t.Run("connection limit reached — entity falls back to ServiceName when no user@db prefix", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:42.000 UTC [1237] FATAL:  remaining connection slots are reserved for non-replication superuser connections`)
		// FATAL lines don't have user@db — entity should be set to the service name
		if ev.Entity == "" {
			t.Error("Entity empty: must fall back to ServiceName for FATAL lines without user@db prefix")
		}
	})

	t.Run("connection limit with user@db present", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:42.000 UTC [1237] appuser@appdb FATAL:  remaining connection slots are reserved for non-replication superuser connections`)
		if ev.Entity != "appdb" {
			t.Errorf("Entity = %q, want appdb", ev.Entity)
		}
	})
}

// ---- Lock timeout -----------------------------------------------------------

func TestMatch_LockTimeout(t *testing.T) {
	t.Run("lock timeout — transaction cancelled waiting for lock", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb ERROR:  canceling statement due to lock timeout`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.NewValue != "lock_timeout" {
			t.Errorf("NewValue = %q, want lock_timeout", ev.NewValue)
		}
		if ev.Entity != "appdb" {
			t.Errorf("Entity = %q, want appdb", ev.Entity)
		}
		if ev.Actor != "appuser" {
			t.Errorf("Actor = %q, want appuser", ev.Actor)
		}
		if ev.Confidence < 0.90 {
			t.Errorf("Confidence = %.2f, want >= 0.90", ev.Confidence)
		}
		if ev.TTLSeconds != 1200 {
			t.Errorf("TTLSeconds = %d, want 1200", ev.TTLSeconds)
		}
	})

	t.Run("lock timeout from write-heavy worker process", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [9999] writer@ordersdb ERROR:  canceling statement due to lock timeout`)
		if ev.Actor != "writer" {
			t.Errorf("Actor = %q, want writer", ev.Actor)
		}
		if ev.Entity != "ordersdb" {
			t.Errorf("Entity = %q, want ordersdb", ev.Entity)
		}
	})

	t.Run("lock timeout differs from deadlock — different failure mode, same saturation type", func(t *testing.T) {
		evLock := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb ERROR:  canceling statement due to lock timeout`)
		evDeadlock := mustMatch(t, `2024-01-15 10:23:41.789 UTC [1236] appuser@appdb ERROR:  deadlock detected`)
		if evLock.NewValue == evDeadlock.NewValue {
			t.Errorf("lock_timeout and deadlock_detected must have different NewValues: both are %q", evLock.NewValue)
		}
		if evDeadlock.Confidence < evLock.Confidence {
			t.Errorf("deadlock (%.2f) should have confidence >= lock_timeout (%.2f): "+
				"deadlock is a circular dependency that aborts transactions; "+
				"lock timeout is a single-direction wait that exceeded config",
				evDeadlock.Confidence, evLock.Confidence)
		}
	})
}

// ---- Statement timeout ------------------------------------------------------

func TestMatch_StatementTimeout(t *testing.T) {
	t.Run("statement timeout — query actively cancelled", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb ERROR:  canceling statement due to statement timeout`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.NewValue != "statement_timeout" {
			t.Errorf("NewValue = %q, want statement_timeout", ev.NewValue)
		}
		if ev.Entity != "appdb" {
			t.Errorf("Entity = %q, want appdb", ev.Entity)
		}
	})

	t.Run("statement timeout is distinct from slow_query — request failed, not just slow", func(t *testing.T) {
		evTimeout := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb ERROR:  canceling statement due to statement timeout`)
		evSlow := mustMatch(t, `2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 4521.234 ms  statement: SELECT 1`)
		if evTimeout.NewValue == evSlow.NewValue {
			t.Errorf("statement_timeout and slow_query must have different NewValues: "+
				"timeout means the request was killed (client gets an error); "+
				"slow query means it completed but took too long")
		}
	})

	t.Run("statement timeout from API layer user", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [5678] api@proddb ERROR:  canceling statement due to statement timeout`)
		if ev.Actor != "api" {
			t.Errorf("Actor = %q, want api", ev.Actor)
		}
		if ev.Entity != "proddb" {
			t.Errorf("Entity = %q, want proddb", ev.Entity)
		}
	})
}

// ---- Temp file (large sort/hash spill) --------------------------------------

func TestMatch_TempFile(t *testing.T) {
	t.Run("large temp file (512MB) — significant sort spill", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  temporary file: path "base/pgsql_tmp/pgsql_tmp1234.0" size 536870912`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if ev.NewValue != "temp_file:512MB" {
			t.Errorf("NewValue = %q, want temp_file:512MB", ev.NewValue)
		}
		if ev.Entity != "appdb" {
			t.Errorf("Entity = %q, want appdb", ev.Entity)
		}
	})

	t.Run("large temp file (1GB) — very large hash join", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  temporary file: path "base/pgsql_tmp/pgsql_tmp5678.0" size 1073741824`)
		if ev.NewValue != "temp_file:1024MB" {
			t.Errorf("NewValue = %q, want temp_file:1024MB", ev.NewValue)
		}
	})

	t.Run("large temp file (100MB threshold) — at boundary", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  temporary file: path "base/pgsql_tmp/pgsql_tmp9999.0" size 104857600`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation at 100MB threshold", ev.EventType)
		}
	})

	t.Run("small temp file below threshold (1MB) — noise", func(t *testing.T) {
		mustNil(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  temporary file: path "base/pgsql_tmp/pgsql_tmp1.0" size 1048576`)
	})

	t.Run("tiny temp file (4KB) — routine, must be nil", func(t *testing.T) {
		mustNil(t, `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  temporary file: path "base/pgsql_tmp/pgsql_tmp2.0" size 4096`)
	})
}

// ---- Slow checkpoint --------------------------------------------------------

func TestMatch_SlowCheckpoint(t *testing.T) {
	t.Run("slow checkpoint (87s) — severe I/O saturation", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint complete: wrote 18421 buffers (28%); write=87.450 s, sync=0.021 s, total=87.471 s; sync files=123, longest=12.345 s, average=3.456 s; distance=98765 kB, estimate=102340 kB`)
		if ev.EventType != schema.EventTypeSaturation {
			t.Errorf("EventType = %q, want saturation", ev.EventType)
		}
		if !strings.HasPrefix(ev.NewValue, "slow_checkpoint:") {
			t.Errorf("NewValue = %q, want slow_checkpoint: prefix", ev.NewValue)
		}
		if !strings.Contains(ev.NewValue, "87.471s") {
			t.Errorf("NewValue = %q, want duration embedded (87.471s)", ev.NewValue)
		}
		if ev.TTLSeconds != 1800 {
			t.Errorf("TTLSeconds = %d, want 1800", ev.TTLSeconds)
		}
	})

	t.Run("slow checkpoint (exactly 10s) — at threshold", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint complete: wrote 5000 buffers (8%); write=9.900 s, sync=0.100 s, total=10.000 s; sync files=50, longest=0.200 s`)
		if ev == nil {
			t.Fatal("expected event at 10.0s threshold, got nil")
		}
		if !strings.Contains(ev.NewValue, "10.000s") {
			t.Errorf("NewValue = %q, should contain 10.000s", ev.NewValue)
		}
	})

	t.Run("fast checkpoint (0.5s) — routine noise, must be nil", func(t *testing.T) {
		mustNil(t, `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint complete: wrote 421 buffers (1%); write=0.234 s, sync=0.002 s, total=0.236 s; sync files=12, longest=0.003 s`)
	})

	t.Run("fast checkpoint (9.9s) — just below threshold, nil", func(t *testing.T) {
		mustNil(t, `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint complete: wrote 3000 buffers (5%); write=9.800 s, sync=0.050 s, total=9.850 s; sync files=30, longest=0.100 s`)
	})

	t.Run("checkpoint starting: time — always noise", func(t *testing.T) {
		mustNil(t, `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint starting: time`)
	})

	t.Run("checkpoint starting: wal — always noise", func(t *testing.T) {
		mustNil(t, `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint starting: wal`)
	})
}

// ---- WAL / Replication ------------------------------------------------------

func TestMatch_ReplicationLag(t *testing.T) {
	t.Run("WAL segment removed before replica consumed it", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [1234] walsender@postgres FATAL:  requested WAL segment 00000001000000000000003A has already been removed`)
		if ev.EventType != schema.EventTypeDependencyFailure {
			t.Errorf("EventType = %q, want dependency_failure: WAL removal means replica must resync from scratch", ev.EventType)
		}
		if ev.NewValue != "replication_slot_invalidated" {
			t.Errorf("NewValue = %q, want replication_slot_invalidated", ev.NewValue)
		}
		if ev.TTLSeconds != 2700 {
			t.Errorf("TTLSeconds = %d, want 2700 (longest TTL — replication lag can cause data divergence)", ev.TTLSeconds)
		}
		if ev.Confidence < 0.88 {
			t.Errorf("Confidence = %.2f, want >= 0.88", ev.Confidence)
		}
	})

	t.Run("WAL segment removed for different segment ID", func(t *testing.T) {
		ev := mustMatch(t, `2024-01-15 10:23:41.000 UTC [5678] walsender@postgres FATAL:  requested WAL segment 00000001000000000000007F has already been removed`)
		if ev.NewValue != "replication_slot_invalidated" {
			t.Errorf("NewValue = %q, want replication_slot_invalidated", ev.NewValue)
		}
	})
}

// ---- Noise ------------------------------------------------------------------

func TestMatch_Noise(t *testing.T) {
	noiseLines := []struct {
		name string
		body string
	}{
		{
			name: "checkpoint starting: time — routine housekeeping",
			body: `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint starting: time`,
		},
		{
			name: "checkpoint starting: wal — triggered by WAL size",
			body: `2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint starting: wal`,
		},
		{
			name: "connection received — client connected, not yet authenticated",
			body: `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  connection received: host=10.0.0.1 port=54321`,
		},
		{
			name: "connection authorized — successful login, routine",
			body: `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  connection authorized: user=appuser database=appdb application_name=myapp`,
		},
		{
			name: "disconnection — session ended, routine",
			body: `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  disconnection: session time: 0:00:01.234 user=appuser database=appdb host=10.0.0.1`,
		},
		{
			name: "database system ready — startup noise",
			body: `2024-01-15 10:23:41.000 UTC [1] LOG:  database system is ready to accept connections`,
		},
		{
			name: "autovacuum launcher started — startup noise",
			body: `2024-01-15 10:23:41.000 UTC [100] LOG:  autovacuum launcher started`,
		},
		{
			name: "database system shut down — maintenance noise",
			body: `2024-01-15 10:23:41.000 UTC [1] LOG:  database system was shut down at 2024-01-15 10:23:40 UTC`,
		},
		{
			name: "statement logging (log_statement = all) — query log noise",
			body: `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  statement: SELECT id FROM users WHERE email = $1`,
		},
		{
			name: "execute prepared statement — query log noise",
			body: `2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  execute stmt1: SELECT id FROM users WHERE email = $1`,
		},
		{
			name: "replication — standby connected (normal operation)",
			body: `2024-01-15 10:23:41.000 UTC [1234] postgres@postgres LOG:  replication connection authorized: user=replicator application_name=standby1`,
		},
		{
			name: "WAL archiving — archive_command success",
			body: `2024-01-15 10:23:41.000 UTC [1] LOG:  archived WAL file "00000001000000000000003A" as "00000001000000000000003A"`,
		},
		{
			name: "statistics collector started — startup noise",
			body: `2024-01-15 10:23:41.000 UTC [101] LOG:  statistics collector started`,
		},
		{
			name: "logical replication worker started — operational",
			body: `2024-01-15 10:23:41.000 UTC [200] LOG:  logical replication apply worker for subscription "my_sub" has started`,
		},
		{
			name: "lock graph detail line — secondary line for a deadlock, already captured on first line",
			body: `2024-01-15 10:23:41.789 UTC [1236] appuser@appdb DETAIL:  Process 1236 waits for ShareLock on transaction 12345`,
		},
		{
			name: "hint line following a deadlock — secondary context",
			body: `2024-01-15 10:23:41.789 UTC [1236] appuser@appdb HINT:  See server log for query details.`,
		},
	}

	for _, tt := range noiseLines {
		t.Run(tt.name, func(t *testing.T) {
			mustNil(t, tt.body)
		})
	}
}

// ---- Cross-contamination guard ---------------------------------------------

func TestPlugin_MatchDoesNotFireOnArgoCDLogs(t *testing.T) {
	argoLines := []string{
		`time="2024-01-15T10:23:41Z" level=info msg="Sync operation starting" app=payment-service dest-server=https://kubernetes.default.svc revision=a1b2c3d4`,
		`time="2024-01-15T10:23:45Z" level=info msg="Sync operation succeeded" app=payment-service revision=a1b2c3d4`,
		`time="2024-01-15T10:24:00Z" level=warning msg="App health changed" app=payment-service from=Healthy to=Degraded`,
		`time="2024-01-15T10:23:41Z" level=debug msg="Watching cluster" server=https://kubernetes.default.svc`,
	}
	for _, body := range argoLines {
		mustNil(t, body)
	}
}

func TestPlugin_MatchDoesNotFireOnKubernetesEvents(t *testing.T) {
	import_ := `{"kind":"Event","reason":"OOMKilling","message":"Memory limit reached. MemoryLimit: 512Mi","type":"Warning","count":1,"lastTimestamp":"2024-01-15T10:23:41Z","involvedObject":{"kind":"Pod","namespace":"production","name":"svc-abc"}}`
	mustNil(t, import_)
}

// ---- RawLine preservation --------------------------------------------------

func TestPlugin_RawLinePreserved(t *testing.T) {
	lines := []struct {
		body    string
		keyword string
	}{
		{`2024-01-15 10:23:41.789 UTC [1236] appuser@appdb ERROR:  deadlock detected`, "deadlock detected"},
		{`2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 4521.234 ms  statement: SELECT 1`, "duration:"},
		{`2024-01-15 10:23:42.000 UTC [1237] FATAL:  remaining connection slots are reserved for non-replication superuser connections`, "remaining connection slots"},
	}

	for _, tt := range lines {
		ev := mustMatch(t, tt.body)
		if !strings.Contains(ev.RawLine, tt.keyword) {
			t.Errorf("RawLine %q does not contain %q", ev.RawLine, tt.keyword)
		}
	}
}

// ---- Schema contract -------------------------------------------------------

func TestPlugin_AllEventsHaveRequiredFields(t *testing.T) {
	representativeLines := []string{
		`2024-01-15 10:23:41.123 UTC [1234] appuser@appdb LOG:  duration: 4521.234 ms  statement: SELECT 1`,
		`2024-01-15 10:23:41.456 UTC [1235] postgres@appdb LOG:  automatic vacuum of table "appdb.public.orders": index scans: 0`,
		`2024-01-15 10:23:41.789 UTC [1236] appuser@appdb ERROR:  deadlock detected`,
		`2024-01-15 10:23:42.000 UTC [1237] FATAL:  remaining connection slots are reserved for non-replication superuser connections`,
		`2024-01-15 10:23:41.000 UTC [1234] appuser@appdb ERROR:  canceling statement due to lock timeout`,
		`2024-01-15 10:23:41.000 UTC [1234] appuser@appdb ERROR:  canceling statement due to statement timeout`,
		`2024-01-15 10:23:41.000 UTC [1234] appuser@appdb LOG:  temporary file: path "base/pgsql_tmp/pgsql_tmp1234.0" size 536870912`,
		`2024-01-15 10:23:41.000 UTC [1] LOG:  checkpoint complete: wrote 18421 buffers (28%); write=87.450 s, sync=0.021 s, total=87.471 s; sync files=123`,
		`2024-01-15 10:23:41.000 UTC [1234] walsender@postgres FATAL:  requested WAL segment 00000001000000000000003A has already been removed`,
	}

	for _, body := range representativeLines {
		ev, err := postgres.New().Match(pg(body))
		if err != nil {
			t.Fatalf("error for %q: %v", body, err)
		}
		if ev == nil {
			t.Fatalf("nil event for %q", body)
		}
		if ev.Vendor != "postgres" {
			t.Errorf("Vendor = %q, want postgres for %q", ev.Vendor, body)
		}
		if ev.EventType == "" {
			t.Errorf("EventType empty for %q", body)
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
