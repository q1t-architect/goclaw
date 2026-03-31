package upgrade

import (
	"errors"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// RunCustomMigrations applies custom migrations from custom-migrations/ directory.
// Uses a separate tracking table "custom_schema_migrations" to avoid conflicts with upstream.
func RunCustomMigrations(dsn string) error {
	dir := ResolveCustomMigrationsDir()

	// Check if directory exists and has migration files.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		slog.Info("no custom migrations directory, skipping")
		return nil
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("custom migrations: connect: %w", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{
		MigrationsTable: "custom_schema_migrations",
	})
	if err != nil {
		return fmt.Errorf("custom migrations: driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+dir,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("custom migrations: create: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("custom migrations: up: %w", err)
	}

	v, dirty, _ := m.Version()
	slog.Info("custom migrations applied", "version", v, "dirty", dirty)
	return nil
}

func ResolveCustomMigrationsDir() string {
	if v := os.Getenv("GOCLAW_CUSTOM_MIGRATIONS_DIR"); v != "" {
		return v
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "custom-migrations")
}
