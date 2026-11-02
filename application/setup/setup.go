package setup

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
)

const (
	DriverSQLite           = "sqlite"
	DriverMySQL            = "mysql"
	MinAdminPasswordLength = 8
)

type MySQLConnection struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

type Request struct {
	DBDriver      string          `json:"db_driver"`
	SQLitePath    string          `json:"sqlite_path"`
	MySQL         MySQLConnection `json:"mysql"`
	StorageRoot   string          `json:"storage_root"`
	AdminEmail    string          `json:"admin_email"`
	AdminPassword string          `json:"admin_password"`
}

type Defaults struct {
	DBDriver    string          `json:"db_driver"`
	SQLitePath  string          `json:"sqlite_path"`
	MySQL       MySQLConnection `json:"mysql"`
	StorageRoot string          `json:"storage_root"`
}

type Status struct {
	Required bool      `json:"required"`
	Defaults *Defaults `json:"defaults,omitempty"`
	// Locked lists request fields fixed by process environment variables.
	Locked []string `json:"locked,omitempty"`
}

type Service interface {
	Status() Status
	TestMySQL(context.Context, MySQLConnection) error
	Complete(context.Context, Request) error
}

func (connection MySQLConnection) Validate() error {
	if strings.TrimSpace(connection.Host) == "" {
		return fault.Validation("mysql host is required")
	}
	if connection.Port == 0 {
		return fault.Validation("mysql port must be between 1 and 65535")
	}
	if strings.TrimSpace(connection.User) == "" {
		return fault.Validation("mysql user is required")
	}
	if strings.TrimSpace(connection.Database) == "" {
		return fault.Validation("mysql database is required")
	}
	return nil
}

func (connection MySQLConnection) URL() string {
	return (&url.URL{
		Scheme: "mysql",
		User:   url.UserPassword(strings.TrimSpace(connection.User), connection.Password),
		Host:   net.JoinHostPort(strings.TrimSpace(connection.Host), strconv.Itoa(int(connection.Port))),
		Path:   "/" + strings.TrimSpace(connection.Database),
	}).String()
}

func (request Request) Validate() error {
	switch request.DBDriver {
	case DriverSQLite:
		if strings.TrimSpace(request.SQLitePath) == "" {
			return fault.Validation("sqlite path is required")
		}
	case DriverMySQL:
		if err := request.MySQL.Validate(); err != nil {
			return err
		}
	default:
		return fault.Validation("database driver must be sqlite or mysql")
	}
	if strings.TrimSpace(request.StorageRoot) == "" {
		return fault.Validation("storage path is required")
	}
	if err := auth.ValidateEmail(strings.TrimSpace(request.AdminEmail)); err != nil {
		return err
	}
	if len(request.AdminPassword) < MinAdminPasswordLength {
		return fault.Validation("admin password must be at least 8 characters")
	}
	return nil
}
