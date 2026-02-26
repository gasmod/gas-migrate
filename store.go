package migrate

import (
	"context"
	"fmt"
	"time"
)

// appliedMigration represents a row in the __gas_migrations tracking table.
type appliedMigration struct {
	AppliedAt      time.Time
	Version        string
	Service        string
	Description    string
	MigrateVersion string
	ModuleVersion  string
	Dirty          bool
}

func (s *Service) createTrackingTable(ctx context.Context) error {
	_, err := s.exec(s.db, ctx, createTrackingTableQuery)
	if err != nil {
		return fmt.Errorf("gas-migrate: failed to create tracking table: %w", err)
	}
	return nil
}

func (s *Service) getAppliedMigrations(ctx context.Context) ([]appliedMigration, error) {
	rows, err := s.query(s.db, ctx, getAppliedMigrationsQuery)
	if err != nil {
		return nil, fmt.Errorf("gas-migrate: failed to query applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var applied []appliedMigration
	for rows.Next() {
		var a appliedMigration
		if err := rows.Scan(&a.Version, &a.Service, &a.Description, &a.MigrateVersion, &a.ModuleVersion, &a.AppliedAt, &a.Dirty); err != nil {
			return nil, fmt.Errorf("gas-migrate: failed to scan migration row: %w", err)
		}
		applied = append(applied, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("gas-migrate: error iterating migration rows: %w", err)
	}
	return applied, nil
}

func (s *Service) getDirtyMigrations(ctx context.Context) ([]appliedMigration, error) {
	rows, err := s.query(s.db, ctx, getDirtyMigrationsQuery)
	if err != nil {
		return nil, fmt.Errorf("gas-migrate: failed to query dirty migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var dirty []appliedMigration
	for rows.Next() {
		var a appliedMigration
		if err := rows.Scan(&a.Version, &a.Service, &a.Description, &a.MigrateVersion, &a.ModuleVersion, &a.AppliedAt, &a.Dirty); err != nil {
			return nil, fmt.Errorf("gas-migrate: failed to scan dirty migration row: %w", err)
		}
		dirty = append(dirty, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("gas-migrate: error iterating dirty migration rows: %w", err)
	}
	return dirty, nil
}

func (s *Service) markApplied(ctx context.Context, version, service, description string) error {
	_, err := s.exec(s.db, ctx, markMigrationAppliedQuery, version, service, description, migrateVersion(), resolveModuleVersion(service))
	if err != nil {
		return fmt.Errorf("gas-migrate: failed to mark migration %s applied: %w", version, err)
	}
	return nil
}

func (s *Service) markDirty(ctx context.Context, version, service, description string) error {
	_, err := s.exec(s.db, ctx, markMigrationDirtyQuery, version, service, description, migrateVersion(), resolveModuleVersion(service))
	if err != nil {
		return fmt.Errorf("gas-migrate: failed to mark migration %s dirty: %w", version, err)
	}
	return nil
}

func (s *Service) removeMigration(ctx context.Context, version string) error {
	_, err := s.exec(s.db, ctx, removeMigrationQuery, version)
	if err != nil {
		return fmt.Errorf("gas-migrate: failed to remove migration %s: %w", version, err)
	}
	return nil
}
