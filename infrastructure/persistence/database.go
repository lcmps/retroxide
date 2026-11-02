package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	sqldriver "github.com/go-sql-driver/mysql"
	"github.com/lcmps/retroxide/application/fault"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	sqliteBusyTimeoutMS = 5000
	// sqliteMaxOpenConns lets WAL readers run concurrently; writers still serialize on SQLite's lock.
	sqliteMaxOpenConns = 4
	mysqlMaxOpenConns  = 10
	mysqlMaxIdleConns  = 5
)

func Initialize(ctx context.Context, db *gorm.DB, driver string) error {
	if db == nil {
		return fault.Validation("database connection is required")
	}
	if db.Dialector.Name() != driver {
		return fault.Validation(fmt.Sprintf("database dialect %q does not match configured driver %q", db.Dialector.Name(), driver))
	}
	maxOpen, maxIdle, err := poolSettings(driver)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fault.Internal(fmt.Errorf("getting database pool: %w", err))
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)

	if driver == "sqlite" {
		// Pragmas come from the DSN (see sqliteDSN), so every pooled connection is configured
		// identically and connections need not be pinned.
		sqlDB.SetConnMaxLifetime(0)
		sqlDB.SetConnMaxIdleTime(0)
	} else {
		sqlDB.SetConnMaxLifetime(5 * time.Minute)
		sqlDB.SetConnMaxIdleTime(time.Minute)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fault.Internal(fmt.Errorf("pinging database: %w", err))
	}
	if err := RunMigrations(ctx, db); err != nil {
		return err
	}
	return nil
}

func OpenDatabase(ctx context.Context, driver, sqlitePath, mysqlURL string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		if sqlitePath == "" {
			return nil, fault.Validation("sqlite path is required")
		}
		dsn, err := sqliteDSN(sqlitePath)
		if err != nil {
			return nil, err
		}
		dialector = sqlite.Open(dsn)
	case "mysql":
		dsn, err := mysqlDSN(mysqlURL)
		if err != nil {
			return nil, err
		}
		dialector = gormmysql.Open(dsn)
	default:
		return nil, fault.Validation("unsupported database driver " + driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		// Multi-statement writes already use explicit transactions; wrapping every single
		// statement in its own transaction only adds round trips.
		//
		// PrepareStmt stays off deliberately: GORM's statement cache holds one mutex while it
		// prepares on the pool, so a transaction waiting for that mutex while holding the last
		// free connection deadlocks the process.
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fault.Internal(fmt.Errorf("opening %s database: %w", driver, err))
	}
	if err := Initialize(ctx, db, driver); err != nil {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	return db, nil
}

// sqliteDSN applies the connection pragmas to every connection the pool opens:
//   - WAL lets readers proceed while a write is in progress, with synchronous=NORMAL, its
//     recommended durability level;
//   - busy_timeout waits for locks instead of failing immediately;
//   - foreign keys are enforced;
//   - transactions begin IMMEDIATE, taking the write lock up front so concurrent transactions
//     queue on busy_timeout rather than failing with "database is locked" when upgrading.
func sqliteDSN(path string) (string, error) {
	if strings.Contains(path, "?") {
		return "", fault.Validation("sqlite path must not contain '?'")
	}
	query := url.Values{}
	for _, pragma := range []string{
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeoutMS),
		"foreign_keys(1)",
	} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	return path + "?" + query.Encode(), nil
}

func PingMySQL(ctx context.Context, mysqlURL string) error {
	dsn, err := mysqlDSN(mysqlURL)
	if err != nil {
		return err
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("opening mysql connection: %w", err)
	}
	defer database.Close()
	return database.PingContext(ctx)
}

func mysqlDSN(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fault.Validation("mysql URL is invalid")
	}
	if parsed.Scheme != "mysql" || parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
		return "", fault.Validation("mysql URL must include mysql scheme, host, and database")
	}
	config := sqldriver.NewConfig()
	if parsed.User != nil {
		config.User = parsed.User.Username()
		config.Passwd, _ = parsed.User.Password()
	}
	config.Net = "tcp"
	config.Addr = parsed.Host
	config.DBName = strings.Trim(parsed.Path, "/")
	config.ParseTime = true
	config.Params = map[string]string{"loc": "UTC"}
	for key, values := range parsed.Query() {
		if len(values) != 0 {
			config.Params[key] = values[len(values)-1]
		}
	}
	return config.FormatDSN(), nil
}

func poolSettings(driver string) (maxOpen, maxIdle int, err error) {
	switch driver {
	case "sqlite":
		return sqliteMaxOpenConns, sqliteMaxOpenConns, nil
	case "mysql":
		return mysqlMaxOpenConns, mysqlMaxIdleConns, nil
	default:
		return 0, 0, fault.Validation("unsupported database driver " + driver)
	}
}
