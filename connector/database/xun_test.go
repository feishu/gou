package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/xun/capsule"
)

func TestXunConnectorPoolSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xun_conn_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbFile := filepath.Join(tmpDir, "test.db")

	x := &Xun{
		Name:   "test_sqlite",
		Driver: "sqlite3",
		Options: XunOptions{
			File:            dbFile,
			MaxOpen:         25,
			MaxIdle:         7,
			IdleTimeout:     45,
			ConnMaxLifetime: 600,
		},
	}

	err = x.setDefaults()
	assert.NoError(t, err)
	assert.Equal(t, 25, x.Options.MaxOpen)
	assert.Equal(t, 7, x.Options.MaxIdle)
	assert.Equal(t, 45, x.Options.IdleTimeout)
	assert.Equal(t, 600, x.Options.ConnMaxLifetime)

	err = x.makeConnections()
	assert.NoError(t, err)
	assert.NotNil(t, x.Manager)
	defer x.Close()

	// Verify pool settings applied to underlying database/sql DB
	found := false
	x.Manager.Connections.Range(func(key, value any) bool {
		if conn, ok := value.(*capsule.Connection); ok {
			found = true
			stats := conn.DB.Stats()
			assert.Equal(t, 25, stats.MaxOpenConnections, "MaxOpenConnections should match configured value")
		}
		return true
	})
	assert.True(t, found, "at least one connection should be loaded in manager")
}

func TestXunConnectorPoolDefaults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xun_conn_defaults_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbFile := filepath.Join(tmpDir, "defaults.db")

	x := &Xun{
		Name:   "test_sqlite_defaults",
		Driver: "sqlite3",
		Options: XunOptions{
			File: dbFile,
		},
	}

	err = x.setDefaults()
	assert.NoError(t, err)
	assert.Equal(t, 50, x.Options.MaxOpen, "default MaxOpen should be 50")
	assert.Equal(t, 10, x.Options.MaxIdle, "default MaxIdle should be 10")
	assert.Equal(t, 180, x.Options.IdleTimeout, "default IdleTimeout should be 180s")
	assert.Equal(t, 1800, x.Options.ConnMaxLifetime, "default Lifetime should be 1800s")

	err = x.makeConnections()
	assert.NoError(t, err)
	defer x.Close()

	x.Manager.Connections.Range(func(key, value any) bool {
		if conn, ok := value.(*capsule.Connection); ok {
			stats := conn.DB.Stats()
			assert.Equal(t, 50, stats.MaxOpenConnections)
		}
		return true
	})
}
