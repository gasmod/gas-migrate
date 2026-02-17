package migrate

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gasmod/gas"
	database "github.com/gasmod/gas-database"
	_ "modernc.org/sqlite"
)

// Compile-time interface checks.
var (
	_ gas.Module           = (*Module)(nil)
	_ gas.MigrationManager = (*Module)(nil)
)

func newTestDB(t *testing.T) gas.DatabaseProvider {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db := database.New(database.WithConfig(&database.Config{
		Driver: "sqlite",
		DSN:    dsn,
	}))
	if err := db.Init(); err != nil {
		t.Fatalf("database Init: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestModule(t *testing.T) (*Module, gas.DatabaseProvider) {
	t.Helper()
	db := newTestDB(t)
	m := New(WithDatabaseProvider(db))
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m, db
}

func TestName(t *testing.T) {
	m := &Module{}
	if m.Name() != "gas-migrate" {
		t.Fatalf("expected gas-migrate, got %s", m.Name())
	}
}

func TestInit_NoDB(t *testing.T) {
	m := New()
	if err := m.Init(); err == nil {
		t.Fatal("expected error for missing DatabaseProvider")
	}
}

func TestInit_CreatesTrackingTable(t *testing.T) {
	m, db := newTestModule(t)
	_ = m

	// Verify the table exists by querying it.
	rows, err := db.Query(context.Background(),
		"SELECT version FROM __gas_migrations")
	if err != nil {
		t.Fatalf("tracking table should exist: %v", err)
	}
	rows.Close()
}

func TestRegister(t *testing.T) {
	m, _ := newTestModule(t)
	m.Register("gas-auth", gas.Migration{
		Version:     "20250216_001",
		Description: "create users table",
		Up:          "CREATE TABLE users (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE users",
	})
	m.Register("gas-auth", gas.Migration{
		Version:     "20250216_002",
		Description: "create sessions table",
		Up:          "CREATE TABLE sessions (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE sessions",
	})
	m.Register("gas-billing", gas.Migration{
		Version:     "20250217_001",
		Description: "create plans table",
		Up:          "CREATE TABLE plans (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE plans",
	})

	if len(m.migrations["gas-auth"]) != 2 {
		t.Errorf("expected 2 auth migrations, got %d", len(m.migrations["gas-auth"]))
	}
	if len(m.migrations["gas-billing"]) != 1 {
		t.Errorf("expected 1 billing migration, got %d", len(m.migrations["gas-billing"]))
	}
	if m.migrations["gas-auth"][0].Module != "gas-auth" {
		t.Error("expected Module field to be set on registration")
	}
}

func TestRunPending(t *testing.T) {
	m, db := newTestModule(t)

	m.Register("gas-auth", gas.Migration{
		Version:     "20250216_001",
		Description: "create users table",
		Up:          "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)",
		Down:        "DROP TABLE users",
	})
	m.Register("gas-billing", gas.Migration{
		Version:     "20250216_002",
		Description: "create plans table",
		Up:          "CREATE TABLE plans (id INTEGER PRIMARY KEY, name TEXT)",
		Down:        "DROP TABLE plans",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	// Verify tables were created.
	ctx := context.Background()
	if _, err := db.Exec(ctx, "INSERT INTO users (id, email) VALUES (1, 'a@b.com')"); err != nil {
		t.Fatalf("users table should exist: %v", err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO plans (id, name) VALUES (1, 'free')"); err != nil {
		t.Fatalf("plans table should exist: %v", err)
	}

	// Verify tracking records.
	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", len(applied))
	}
	if applied[0].Version != "20250216_001" {
		t.Errorf("first applied = %s, want 20250216_001", applied[0].Version)
	}
	if applied[1].Version != "20250216_002" {
		t.Errorf("second applied = %s, want 20250216_002", applied[1].Version)
	}
}

func TestRunPending_SkipsApplied(t *testing.T) {
	m, _ := newTestModule(t)

	m.Register("mod-a", gas.Migration{
		Version:     "20250216_001",
		Description: "first",
		Up:          "CREATE TABLE first_table (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE first_table",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("first RunPending: %v", err)
	}

	// Register another migration and run again.
	m.Register("mod-a", gas.Migration{
		Version:     "20250216_002",
		Description: "second",
		Up:          "CREATE TABLE second_table (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE second_table",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("second RunPending: %v", err)
	}

	applied, err := m.getAppliedMigrations(context.Background())
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied, got %d", len(applied))
	}
}

func TestRunPending_DirtyBlocks(t *testing.T) {
	m, _ := newTestModule(t)

	ctx := context.Background()
	if err := m.markDirty(ctx, "20250216_001", "mod-a", "broken migration"); err != nil {
		t.Fatalf("markDirty: %v", err)
	}

	m.Register("mod-a", gas.Migration{
		Version:     "20250216_002",
		Description: "should not run",
		Up:          "CREATE TABLE should_not_exist (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE should_not_exist",
	})

	if err := m.RunPending(); err == nil {
		t.Fatal("expected error due to dirty migration")
	}
}

func TestRunPending_FailedMigrationMarksDirty(t *testing.T) {
	m, _ := newTestModule(t)

	m.Register("mod-a", gas.Migration{
		Version:     "20250216_001",
		Description: "invalid SQL",
		Up:          "THIS IS NOT VALID SQL",
		Down:        "SELECT 1",
	})

	err := m.RunPending()
	if err == nil {
		t.Fatal("expected error for invalid SQL")
	}

	dirty, err := m.getDirtyMigrations(context.Background())
	if err != nil {
		t.Fatalf("getDirtyMigrations: %v", err)
	}
	if len(dirty) != 1 {
		t.Fatalf("expected 1 dirty migration, got %d", len(dirty))
	}
	if dirty[0].Version != "20250216_001" {
		t.Errorf("dirty version = %s, want 20250216_001", dirty[0].Version)
	}
}

func TestDown(t *testing.T) {
	m, db := newTestModule(t)

	m.Register("mod-a", gas.Migration{
		Version:     "20250216_001",
		Description: "create table a",
		Up:          "CREATE TABLE table_a (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE table_a",
	})
	m.Register("mod-a", gas.Migration{
		Version:     "20250216_002",
		Description: "create table b",
		Up:          "CREATE TABLE table_b (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE table_b",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	// Roll back the last migration.
	if err := m.Down(1); err != nil {
		t.Fatalf("Down(1): %v", err)
	}

	ctx := context.Background()

	// table_a should still exist.
	if _, err := db.Exec(ctx, "INSERT INTO table_a (id) VALUES (1)"); err != nil {
		t.Fatalf("table_a should still exist: %v", err)
	}

	// table_b should be gone.
	_, err := db.Exec(ctx, "INSERT INTO table_b (id) VALUES (1)")
	if err == nil {
		t.Fatal("table_b should have been dropped")
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied after Down(1), got %d", len(applied))
	}
}

func TestDown_AllMigrations(t *testing.T) {
	m, _ := newTestModule(t)

	m.Register("mod-a", gas.Migration{
		Version:     "20250216_001",
		Description: "create table",
		Up:          "CREATE TABLE down_all (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE down_all",
	})
	m.Register("mod-a", gas.Migration{
		Version:     "20250216_002",
		Description: "create table 2",
		Up:          "CREATE TABLE down_all2 (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE down_all2",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	if err := m.Down(2); err != nil {
		t.Fatalf("Down(2): %v", err)
	}

	applied, err := m.getAppliedMigrations(context.Background())
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected 0 applied after Down(2), got %d", len(applied))
	}
}

func TestDown_MoreThanApplied(t *testing.T) {
	m, _ := newTestModule(t)

	m.Register("mod-a", gas.Migration{
		Version:     "20250216_001",
		Description: "create table",
		Up:          "CREATE TABLE down_extra (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE down_extra",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	// Asking to roll back more than applied should just roll back what exists.
	if err := m.Down(5); err != nil {
		t.Fatalf("Down(5): %v", err)
	}

	applied, err := m.getAppliedMigrations(context.Background())
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected 0 applied, got %d", len(applied))
	}
}

func TestRunPending_Closed(t *testing.T) {
	m, _ := newTestModule(t)
	m.Close()

	if err := m.RunPending(); err == nil {
		t.Fatal("expected error when module is closed")
	}
}

func TestDown_Closed(t *testing.T) {
	m, _ := newTestModule(t)
	m.Close()

	if err := m.Down(1); err == nil {
		t.Fatal("expected error when module is closed")
	}
}

func TestGlobalVersionOrder(t *testing.T) {
	m, db := newTestModule(t)

	// Register out of order across modules.
	m.Register("mod-b", gas.Migration{
		Version:     "20250216_002",
		Description: "mod-b first",
		Up:          "CREATE TABLE mod_b_first (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE mod_b_first",
	})
	m.Register("mod-a", gas.Migration{
		Version:     "20250216_001",
		Description: "mod-a first",
		Up:          "CREATE TABLE mod_a_first (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE mod_a_first",
	})
	m.Register("mod-a", gas.Migration{
		Version:     "20250216_003",
		Description: "mod-a second",
		Up:          "CREATE TABLE mod_a_second (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE mod_a_second",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	applied, err := m.getAppliedMigrations(context.Background())
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 applied, got %d", len(applied))
	}

	// Verify order: 001 (mod-a), 002 (mod-b), 003 (mod-a).
	expected := []struct {
		version string
		module  string
	}{
		{"20250216_001", "mod-a"},
		{"20250216_002", "mod-b"},
		{"20250216_003", "mod-a"},
	}
	for i, exp := range expected {
		if applied[i].Version != exp.version || applied[i].Module != exp.module {
			t.Errorf("applied[%d] = (%s, %s), want (%s, %s)",
				i, applied[i].Version, applied[i].Module, exp.version, exp.module)
		}
	}

	// Verify all tables exist.
	ctx := context.Background()
	for _, table := range []string{"mod_a_first", "mod_b_first", "mod_a_second"} {
		if _, err := db.Exec(ctx, "SELECT 1 FROM "+table+" LIMIT 1"); err != nil {
			t.Errorf("table %s should exist: %v", table, err)
		}
	}
}

func TestRegisterSlice(t *testing.T) {
	m, db := newTestModule(t)

	m.RegisterSlice("mod-a", []gas.Migration{
		{
			Version:     "20250216_001",
			Description: "create table x",
			Up:          "CREATE TABLE table_x (id INTEGER PRIMARY KEY)",
			Down:        "DROP TABLE table_x",
		},
		{
			Version:     "20250216_002",
			Description: "create table y",
			Up:          "CREATE TABLE table_y (id INTEGER PRIMARY KEY)",
			Down:        "DROP TABLE table_y",
		},
	})

	if len(m.migrations["mod-a"]) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(m.migrations["mod-a"]))
	}
	if m.migrations["mod-a"][0].Module != "mod-a" {
		t.Error("expected Module field to be set")
	}

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	ctx := context.Background()
	if _, err := db.Exec(ctx, "SELECT 1 FROM table_x LIMIT 1"); err != nil {
		t.Fatalf("table_x should exist: %v", err)
	}
	if _, err := db.Exec(ctx, "SELECT 1 FROM table_y LIMIT 1"); err != nil {
		t.Fatalf("table_y should exist: %v", err)
	}
}

func TestRegisterFS(t *testing.T) {
	m, db := newTestModule(t)

	fsys := fstest.MapFS{
		"20250216_001_create_accounts.up.sql":   {Data: []byte("CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT)")},
		"20250216_001_create_accounts.down.sql": {Data: []byte("DROP TABLE accounts")},
		"20250216_002_create_orders.up.sql":     {Data: []byte("CREATE TABLE orders (id INTEGER PRIMARY KEY, total INTEGER)")},
		"20250216_002_create_orders.down.sql":   {Data: []byte("DROP TABLE orders")},
	}

	if err := m.RegisterFS("mod-fs", fsys); err != nil {
		t.Fatalf("RegisterFS: %v", err)
	}

	if len(m.migrations["mod-fs"]) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(m.migrations["mod-fs"]))
	}

	// Check parsed version and description.
	mig := m.migrations["mod-fs"][0]
	if mig.Version != "20250216_001" {
		t.Errorf("version = %q, want 20250216_001", mig.Version)
	}
	if mig.Description != "create accounts" {
		t.Errorf("description = %q, want 'create accounts'", mig.Description)
	}
	if mig.Module != "mod-fs" {
		t.Errorf("module = %q, want mod-fs", mig.Module)
	}

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	ctx := context.Background()
	if _, err := db.Exec(ctx, "INSERT INTO accounts (id, name) VALUES (1, 'test')"); err != nil {
		t.Fatalf("accounts table should exist: %v", err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO orders (id, total) VALUES (1, 100)"); err != nil {
		t.Fatalf("orders table should exist: %v", err)
	}
}

func TestRegisterFS_MissingDown(t *testing.T) {
	m, _ := newTestModule(t)

	fsys := fstest.MapFS{
		"20250216_001_orphan.up.sql": {Data: []byte("CREATE TABLE orphan (id INTEGER PRIMARY KEY)")},
	}

	if err := m.RegisterFS("mod-fs", fsys); err == nil {
		t.Fatal("expected error for missing down file")
	}
}

func TestRegisterFS_DownOnlyIgnored(t *testing.T) {
	m, _ := newTestModule(t)

	// A .down.sql without a matching .up.sql should be silently ignored
	// (we only glob for *.up.sql).
	fsys := fstest.MapFS{
		"20250216_001_orphan.down.sql": {Data: []byte("DROP TABLE orphan")},
	}

	if err := m.RegisterFS("mod-fs", fsys); err != nil {
		t.Fatalf("RegisterFS: %v", err)
	}

	if len(m.migrations["mod-fs"]) != 0 {
		t.Fatalf("expected 0 migrations, got %d", len(m.migrations["mod-fs"]))
	}
}

func TestVersionColumnsStored(t *testing.T) {
	m, _ := newTestModule(t)

	m.Register("gas-auth", gas.Migration{
		Version:     "20250216_001",
		Description: "create table",
		Up:          "CREATE TABLE ver_test (id INTEGER PRIMARY KEY)",
		Down:        "DROP TABLE ver_test",
	})

	if err := m.RunPending(); err != nil {
		t.Fatalf("RunPending: %v", err)
	}

	applied, err := m.getAppliedMigrations(context.Background())
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(applied))
	}

	// In tests, build info may not resolve versions (go test doesn't embed
	// full module versions), so we just verify the columns are scannable
	// and don't panic. The values will be empty strings in test context.
	_ = applied[0].MigrateVersion
	_ = applied[0].ModuleVersion
}

func TestParseStem(t *testing.T) {
	tests := []struct {
		stem        string
		wantVersion string
		wantDesc    string
	}{
		{"20250216_001_create_users", "20250216_001", "create users"},
		{"20250216_002_add_email_to_users", "20250216_002", "add email to users"},
		{"20250216_003", "20250216_003", ""},
		{"single", "single", ""},
	}

	for _, tt := range tests {
		version, desc := parseStem(tt.stem)
		if version != tt.wantVersion || desc != tt.wantDesc {
			t.Errorf("parseStem(%q) = (%q, %q), want (%q, %q)",
				tt.stem, version, desc, tt.wantVersion, tt.wantDesc)
		}
	}
}
