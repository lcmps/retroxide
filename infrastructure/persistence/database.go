package persistence

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	sqldriver "github.com/go-sql-driver/mysql"
	"github.com/lcmps/retroxide/application/fault"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	sqliteBusyTimeoutMS = 5000
	mysqlMaxOpenConns   = 10
	mysqlMaxIdleConns   = 5
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
		// SQLite pragmas are connection-scoped; keep the single connection alive.
		sqlDB.SetConnMaxLifetime(0)
		sqlDB.SetConnMaxIdleTime(0)
		for _, pragma := range []string{
			"PRAGMA journal_mode=WAL",
			fmt.Sprintf("PRAGMA busy_timeout=%d", sqliteBusyTimeoutMS),
			"PRAGMA foreign_keys=ON",
		} {
			if err := db.WithContext(ctx).Exec(pragma).Error; err != nil {
				return fault.Internal(fmt.Errorf("configuring sqlite: %w", err))
			}
		}
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
		dialector = sqlite.Open(sqlitePath)
	case "mysql":
		dsn, err := mysqlDSN(mysqlURL)
		if err != nil {
			return nil, err
		}
		dialector = gormmysql.Open(dsn)
	default:
		return nil, fault.Validation("unsupported database driver " + driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
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
		return 1, 1, nil
	case "mysql":
		return mysqlMaxOpenConns, mysqlMaxIdleConns, nil
	default:
		return 0, 0, fault.Validation("unsupported database driver " + driver)
	}
}
