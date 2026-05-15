package client

import (
	"testing"

	gatewaydb "github.com/obot-platform/obot/pkg/gateway/db"
	sservices "github.com/obot-platform/obot/pkg/storage/services"
)

// NewForTest returns a Client backed by an in-memory SQLite gateway db
// with all migrations applied. Background goroutines are not started.
// Exported so packages above this one can drive end-to-end flows
// (handler → gateway → db) without booting a full server.
func NewForTest(t *testing.T) *Client {
	t.Helper()
	// _pragma=foreign_keys(1) makes SQLite honor the cascade-delete
	// constraints declared on the gateway types, matching Postgres
	// behavior in production.
	services, err := sservices.New(sservices.Config{DSN: "sqlite://file::memory:?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("storage services: %v", err)
	}
	db, err := gatewaydb.New(services.DB.DB, services.DB.SQLDB, true)
	if err != nil {
		t.Fatalf("gateway db: %v", err)
	}
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	return &Client{db: db}
}
