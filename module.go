package migrate

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gasmod/gas"
)

// Module implements gas.Module and gas.MigrationManager.
// It tracks database migrations across all Gas modules, applying
// pending migrations on startup and supporting rollback operations.
type Module struct {
	db         gas.DatabaseProvider
	eventBus   *gas.EventBus
	migrations map[string][]gas.Migration
	closed     atomic.Bool
	mu         sync.Mutex
}

// Option configures the Module.
type Option func(*Module)

// WithDatabaseProvider sets the database provider used for running
// migrations and tracking applied state.
func WithDatabaseProvider(db gas.DatabaseProvider) Option {
	return func(m *Module) { m.db = db }
}

// WithEventBus sets the event bus for module lifecycle events.
func WithEventBus(bus *gas.EventBus) Option {
	return func(m *Module) { m.eventBus = bus }
}

// New creates a new migration manager module.
func New(opts ...Option) *Module {
	m := &Module{
		migrations: make(map[string][]gas.Migration),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Name returns the module identifier.
func (m *Module) Name() string { return "gas-migrate" }

// Init validates dependencies and creates the migrations tracking table.
func (m *Module) Init() error {
	if m.db == nil {
		return fmt.Errorf("gas-migrate: DatabaseProvider is required")
	}
	return m.createTrackingTable(context.Background())
}

// Close marks the module as closed.
func (m *Module) Close() error {
	m.closed.Store(true)
	return nil
}
