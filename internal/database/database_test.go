package database

import (
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestDSNPreservesSpecialCredentialsAndTimeouts(t *testing.T) {
	input := Config{
		Host:           "db.internal",
		Port:           3307,
		User:           "app-user",
		Password:       "p@ss:/?#&word",
		DBName:         "sync_hub",
		Charset:        "utf8mb4",
		ConnectTimeout: 11 * time.Second,
		ReadTimeout:    31 * time.Second,
		WriteTimeout:   32 * time.Second,
	}
	parsed, err := mysqlDriver.ParseDSN(DSN(input))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Passwd != input.Password || parsed.User != input.User || parsed.Addr != "db.internal:3307" {
		t.Fatalf("DSN did not round-trip credentials/address: %+v", parsed)
	}
	if parsed.Timeout != input.ConnectTimeout || parsed.ReadTimeout != input.ReadTimeout || parsed.WriteTimeout != input.WriteTimeout {
		t.Fatalf("DSN did not retain timeouts: %+v", parsed)
	}
	if parsed.Loc != time.UTC || parsed.Params["time_zone"] != "'+00:00'" {
		t.Fatalf("DSN must normalize application and MySQL session timestamps to UTC: %+v", parsed)
	}
}
