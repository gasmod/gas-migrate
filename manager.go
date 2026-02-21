package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gasmod/gas"
)

// Register adds a migration owned by the given service.
func (s *Service) Register(module string, migration gas.Migration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	migration.Module = module
	s.migrations[module] = append(s.migrations[module], migration)
}

// RegisterSlice adds multiple migrations at once for the given service.
func (s *Service) RegisterSlice(module string, migrations []gas.Migration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, migration := range migrations {
		migration.Module = module
		s.migrations[module] = append(s.migrations[module], migration)
	}
}

// RegisterFS reads migration files from an fs.FS and registers them for the
// given module. Files must follow the naming convention:
//
//	{version}_{description}.up.sql   — the up (apply) SQL
//	{version}_{description}.down.sql — the down (rollback) SQL
//
// The version is extracted as the YYYYMMDD_NNN prefix (digits_digits), and
// the description is the remaining underscored segment converted to spaces.
// Every .up.sql file must have a matching .down.sql file.
func (s *Service) RegisterFS(module string, fsys fs.FS) error {
	pairs, err := parseMigrationFS(fsys)
	if err != nil {
		return fmt.Errorf("gas-migrate: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range pairs {
		p.migration.Module = module
		s.migrations[module] = append(s.migrations[module], p.migration)
	}
	return nil
}

type migrationPair struct {
	migration gas.Migration
}

func parseMigrationFS(fsys fs.FS) ([]migrationPair, error) {
	upFiles, err := fs.Glob(fsys, "*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("failed to glob up files: %w", err)
	}

	sort.Strings(upFiles)

	pairs := make([]migrationPair, 0, len(upFiles))
	for _, upFile := range upFiles {
		stem := strings.TrimSuffix(upFile, ".up.sql")
		downFile := stem + ".down.sql"

		upSQL, err := fs.ReadFile(fsys, upFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", upFile, err)
		}

		downSQL, err := fs.ReadFile(fsys, downFile)
		if err != nil {
			return nil, fmt.Errorf("missing down file for %s: %w", upFile, err)
		}

		version, description := parseStem(stem)

		pairs = append(pairs, migrationPair{
			migration: gas.Migration{
				Version:     version,
				Description: description,
				Up:          string(upSQL),
				Down:        string(downSQL),
			},
		})
	}

	return pairs, nil
}

// parseStem extracts version and description from a migration filename stem.
// Given "20250216_001_create_users_table", it returns version="20250216_001"
// and description="create users table".
func parseStem(stem string) (version, description string) {
	// Strip any directory prefix.
	stem = filepath.Base(stem)

	// Split into parts by underscore.
	parts := strings.Split(stem, "_")

	// The version is the first two underscore-separated segments (YYYYMMDD_NNN).
	if len(parts) < 2 {
		return stem, ""
	}

	version = parts[0] + "_" + parts[1]
	if len(parts) > 2 {
		description = strings.Join(parts[2:], " ")
	}
	return version, description
}

// RunPending applies all unapplied migrations in global version order.
// If any migration is marked dirty, execution is blocked until resolved.
func (s *Service) RunPending() error {
	if s.closed.Load() {
		return fmt.Errorf("gas-migrate: service is closed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()

	dirty, err := s.getDirtyMigrations(ctx)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		versions := make([]string, len(dirty))
		for i, d := range dirty {
			versions[i] = d.Version
		}
		return fmt.Errorf("gas-migrate: dirty migrations block execution: %s", strings.Join(versions, ", "))
	}

	applied, err := s.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}
	appliedSet := make(map[string]struct{}, len(applied))
	for _, a := range applied {
		appliedSet[a.Version] = struct{}{}
	}

	all := s.allMigrationsSorted()

	for _, migration := range all {
		if _, ok := appliedSet[migration.Version]; ok {
			continue
		}
		if err := s.applyUp(ctx, migration); err != nil {
			return err
		}
	}

	return nil
}

// Down reverses the last n applied migrations in reverse version order.
func (s *Service) Down(n int) error {
	if s.closed.Load() {
		return fmt.Errorf("gas-migrate: service is closed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()

	applied, err := s.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	registered := s.migrationsByVersion()

	// Reverse order: most recently applied first.
	count := n
	if count > len(applied) {
		count = len(applied)
	}

	for i := len(applied) - 1; i >= len(applied)-count; i-- {
		a := applied[i]
		migration, ok := registered[a.Version]
		if !ok {
			return fmt.Errorf("gas-migrate: applied migration %s not found in registered migrations", a.Version)
		}
		if err := s.applyDown(ctx, migration); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) applyUp(ctx context.Context, migration gas.Migration) error {
	tx, err := s.db.DB().BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("gas-migrate: failed to begin transaction for %s: %w", migration.Version, err)
	}

	if _, err := tx.ExecContext(ctx, migration.Up); err != nil {
		_ = tx.Rollback()
		if markErr := s.markDirty(ctx, migration.Version, migration.Module, migration.Description); markErr != nil {
			return fmt.Errorf("gas-migrate: migration %s failed: %w (also failed to mark dirty: %w)",
				migration.Version, err, markErr)
		}
		return fmt.Errorf("gas-migrate: migration %s failed (marked dirty): %w", migration.Version, err)
	}

	if err := tx.Commit(); err != nil {
		if markErr := s.markDirty(ctx, migration.Version, migration.Module, migration.Description); markErr != nil {
			return fmt.Errorf("gas-migrate: commit failed for %s: %w (also failed to mark dirty: %w)",
				migration.Version, err, markErr)
		}
		return fmt.Errorf("gas-migrate: commit failed for %s (marked dirty): %w", migration.Version, err)
	}

	return s.markApplied(ctx, migration.Version, migration.Module, migration.Description)
}

func (s *Service) applyDown(ctx context.Context, migration gas.Migration) error {
	tx, err := s.db.DB().BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("gas-migrate: failed to begin transaction for down %s: %w", migration.Version, err)
	}

	if _, err := tx.ExecContext(ctx, migration.Down); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("gas-migrate: down migration %s failed: %w", migration.Version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gas-migrate: commit failed for down %s: %w", migration.Version, err)
	}

	return s.removeMigration(ctx, migration.Version)
}

func (s *Service) allMigrationsSorted() []gas.Migration {
	total := 0
	for _, migs := range s.migrations {
		total += len(migs)
	}
	all := make([]gas.Migration, 0, total)
	for _, migs := range s.migrations {
		all = append(all, migs...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Version < all[j].Version
	})
	return all
}

func (s *Service) migrationsByVersion() map[string]gas.Migration {
	byVersion := make(map[string]gas.Migration)
	for _, migs := range s.migrations {
		for _, mig := range migs {
			byVersion[mig.Version] = mig
		}
	}
	return byVersion
}
