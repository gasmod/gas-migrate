package migrate

//goland:noinspection SqlNoDataSourceInspection
const createTrackingTableQuery = `CREATE TABLE IF NOT EXISTS __gas_migrations
(
    version         TEXT PRIMARY KEY,
    service         TEXT      NOT NULL,
    description     TEXT      NOT NULL DEFAULT '',
    migrate_version TEXT      NOT NULL DEFAULT '',
    module_version  TEXT      NOT NULL DEFAULT '',
    applied_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    dirty           BOOLEAN   NOT NULL DEFAULT FALSE
)`

//goland:noinspection SqlNoDataSourceInspection,SqlResolve
const getAppliedMigrationsQuery = `SELECT version,
       service,
       description,
       migrate_version,
       module_version,
       applied_at,
       dirty
FROM __gas_migrations
ORDER BY version`

//goland:noinspection SqlNoDataSourceInspection,SqlResolve
const getDirtyMigrationsQuery = `SELECT version,
       service,
       description,
       migrate_version,
       module_version,
       applied_at,
       dirty
FROM __gas_migrations
WHERE dirty = TRUE
ORDER BY version`

//goland:noinspection SqlNoDataSourceInspection,SqlResolve
const markMigrationAppliedQuery = `INSERT INTO __gas_migrations (version, service, description, migrate_version, module_version)
VALUES (?, ?, ?, ?, ?)`

//goland:noinspection SqlNoDataSourceInspection,SqlResolve
const markMigrationDirtyQuery = `INSERT INTO __gas_migrations (version, service, description, migrate_version, module_version, dirty)
VALUES (?, ?, ?, ?, ?, TRUE)
ON CONFLICT (version) DO UPDATE SET dirty = TRUE`

//goland:noinspection SqlNoDataSourceInspection,SqlResolve
const removeMigrationQuery = `DELETE
FROM __gas_migrations
WHERE version = ?`
