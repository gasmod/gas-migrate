---
name: gas-migrate
description: >
  Reference documentation for the gas-migrate Go package
  (github.com/gasmod/gas-migrate) — the migration manager for the Gas
  ecosystem. Use this skill when writing, reviewing, or debugging Go code
  involving database migrations in Gas services. Covers migration registration
  (single, slice, embedded FS), RunPending, Down/rollback, dirty-state
  handling, version naming conventions, the __gas_migrations tracking table,
  and DI wiring with gas-database.
---

# Gas Migrate Package Reference

Migration manager for the Gas ecosystem. Tracks and applies database migrations
across all Gas services with dirty-state detection and rollback support.

```
import migrate "github.com/gasmod/gas-migrate"
```

## Constructor

```go
func New() func(gas.DatabaseProvider) *Service
```

Returns a DI-injectable constructor. Receives `gas.DatabaseProvider` from the
container (i.e. depends on `gas-database`).

## Service

Implements `gas.Service` and `gas.MigrationManager`.

### Lifecycle

```go
func (s *Service) Name() string   // "gas-migrate"
func (s *Service) Init() error    // validates deps, creates __gas_migrations table
func (s *Service) Close() error   // marks service as closed
```

### Registering Migrations

Services register their migrations during `Init()`. Three approaches:

```go
// Single migration
func (s *Service) Register(service string, migration gas.Migration)

// Batch
func (s *Service) RegisterSlice(service string, migrations []gas.Migration)

// Embedded SQL files
func (s *Service) RegisterFS(service string, fsys fs.FS) error
```

### Executing Migrations

```go
func (s *Service) RunPending() error  // apply all unapplied in global version order
func (s *Service) Down(n int) error   // reverse last n applied in reverse version order
```

## Migration Struct (defined in gas core)

```go
type Migration struct {
    Version     string  // e.g. "20250216_001"
    Service      string  // owning service name
    Description string  // human-readable
    Up          string  // apply SQL
    Down        string  // rollback SQL
}
```

## Version Naming Convention

Format: `YYYYMMDD_NNN` — date prefix + sequence number.

```
20250216_001  — first migration on Feb 16, 2025
20250216_002  — second migration same day
20250301_001  — first migration on Mar 1, 2025
```

Migrations are sorted globally by version across all services.

## Embedded SQL Files (RegisterFS)

File naming: `{version}_{description}.up.sql` / `{version}_{description}.down.sql`

```
migrations/
    20250216_001_create_users.up.sql
    20250216_001_create_users.down.sql
    20250216_002_create_sessions.up.sql
    20250216_002_create_sessions.down.sql
```

Version is the `YYYYMMDD_NNN` prefix. Description is parsed from remaining
segments (underscores → spaces). Every `.up.sql` must have a matching `.down.sql`.

Usage with `embed`:

```go
import "embed"

//go:embed migrations/*.sql
var migrationsFS embed.FS

func (s *Service) Init() error {
    return s.migrationMgr.RegisterFS(s.Name(), migrationsFS)
}
```

## How It Works

- Tracked in a `__gas_migrations` table (created automatically on `Init()`).
- `RunPending()` sorts all registered migrations globally by version and
  applies unapplied ones. Each migration runs in its own transaction.
- If a migration fails, it is marked **dirty** — all further execution is
  blocked until the dirty state is manually resolved.
- `Down(n)` reverses the last `n` applied migrations in reverse version order.

## Dirty State Resolution

When a migration fails:

1. Fix the underlying issue (bad SQL, missing dependency).
2. Manually remove or update the dirty row in `__gas_migrations`.
3. Run `RunPending()` again.

## DI Wiring

```go
app := gas.NewApp(
    gas.WithService[*database.Service](
        database.New(),
        gas.ServiceLifetimeSingleton,
    ),
    gas.WithService[*migrate.Service](
        migrate.New(),
        gas.ServiceLifetimeSingleton,
    ),
    gas.WithService[*auth.Service](
        auth.New,
        gas.ServiceLifetimeSingleton,
    ),
)
```

`migrate.Service` depends on `gas.DatabaseProvider`, so `gas-database` must be
registered. The DI container handles ordering automatically.

## Consuming in a Service

```go
type Service struct {
    migrationMgr gas.MigrationManager
    router       *gas.Router
}

func New(mgr gas.MigrationManager, router *gas.Router) *Service {
    return &Service{migrationMgr: mgr, router: router}
}

func (s *Service) Init() error {
    s.migrationMgr.RegisterSlice(s.Name(), []gas.Migration{
        {
            Version:     "20250216_001",
            Description: "create users table",
            Up:          "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL);",
            Down:        "DROP TABLE users;",
        },
    })
    // register routes, subscriptions, etc.
    return nil
}
```
