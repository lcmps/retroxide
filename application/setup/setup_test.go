package setup

import (
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
)

var validMySQL = MySQLConnection{Host: "db.local", Port: 3306, User: "retro", Password: "p@ss/word", Database: "retroxide"}

func TestMySQLConnectionValidateRequiresEveryField(t *testing.T) {
	if err := validMySQL.Validate(); err != nil {
		t.Fatalf("valid connection = %v", err)
	}
	for name, change := range map[string]func(*MySQLConnection){
		"host":     func(c *MySQLConnection) { c.Host = "  " },
		"port":     func(c *MySQLConnection) { c.Port = 0 },
		"user":     func(c *MySQLConnection) { c.User = "" },
		"database": func(c *MySQLConnection) { c.Database = " " },
	} {
		connection := validMySQL
		change(&connection)
		if err := connection.Validate(); !fault.IsCode(err, fault.CodeValidation) || !strings.Contains(err.Error(), name) {
			t.Errorf("missing %s: Validate() = %v, want a validation error naming it", name, err)
		}
	}
}

func TestMySQLConnectionURLEscapesCredentialsAndTrimsFields(t *testing.T) {
	connection := validMySQL
	connection.Host, connection.User, connection.Database = " db.local ", " retro ", " retroxide "
	if got, want := connection.URL(), "mysql://retro:p%40ss%2Fword@db.local:3306/retroxide"; got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestRequestValidate(t *testing.T) {
	valid := Request{DBDriver: DriverSQLite, SQLitePath: "./retroxide.sqlite3", StorageRoot: "./storage", AdminEmail: "admin@example.com", AdminPassword: "longenough"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid sqlite request = %v", err)
	}
	mysql := valid
	mysql.DBDriver, mysql.SQLitePath, mysql.MySQL = DriverMySQL, "", validMySQL
	if err := mysql.Validate(); err != nil {
		t.Fatalf("valid mysql request = %v", err)
	}
	for name, change := range map[string]func(*Request){
		"sqlite path": func(r *Request) { r.SQLitePath = " " },
		"mysql host": func(r *Request) {
			r.DBDriver, r.MySQL = DriverMySQL, MySQLConnection{Port: 3306, User: "u", Database: "d"}
		},
		"driver":         func(r *Request) { r.DBDriver = "postgres" },
		"storage path":   func(r *Request) { r.StorageRoot = "" },
		"email":          func(r *Request) { r.AdminEmail = "not-an-email" },
		"admin password": func(r *Request) { r.AdminPassword = "short" },
	} {
		request := valid
		change(&request)
		if err := request.Validate(); !fault.IsCode(err, fault.CodeValidation) {
			t.Errorf("%s: Validate() = %v, want a validation error", name, err)
		}
	}
	trimmed := valid
	trimmed.AdminEmail = "  admin@example.com  "
	if err := trimmed.Validate(); err != nil {
		t.Fatalf("email with surrounding spaces = %v, want accepted after trimming", err)
	}
}
