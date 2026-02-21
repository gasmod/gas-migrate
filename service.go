package migrate

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gasmod/gas"
)

// Service implements gas.Service and gas.MigrationManager.
// It tracks database migrations across all Gas services, applying
// pending migrations on startup and supporting rollback operations.
type Service struct {
	db         gas.DatabaseProvider
	migrations map[string][]gas.Migration
	closed     atomic.Bool
	mu         sync.Mutex
}

// New returns a DI-injectable constructor for the migration manager service.
func New() func(gas.DatabaseProvider) *Service {
	return func(db gas.DatabaseProvider) *Service {
		return &Service{
			db:         db,
			migrations: make(map[string][]gas.Migration),
		}
	}
}

// Name returns the service identifier.
func (s *Service) Name() string { return "gas-migrate" }

// Init validates dependencies and creates the migrations tracking table.
func (s *Service) Init() error {
	if s.db == nil {
		return fmt.Errorf("%s: DatabaseProvider is required", s.Name())
	}
	return s.createTrackingTable(context.Background())
}

// Close marks the service as closed.
func (s *Service) Close() error {
	s.closed.Store(true)
	return nil
}
