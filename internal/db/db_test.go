package db

import (
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_MemoryDB(t *testing.T) {
	d, err := Open("file::memory:?cache=shared")
	require.NoError(t, err)
	require.NotNil(t, d)
	t.Cleanup(func() { d.Close() })

	err = d.Ping()
	assert.NoError(t, err)
}

func TestOpen_InvalidDSN(t *testing.T) {
	d, err := Open("\x00invalid")
	assert.Error(t, err)
	assert.Nil(t, d)
}

func TestInitSchema(t *testing.T) {
	d, err := Open("file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	err = InitSchema(d)
	require.NoError(t, err)

	rows, err := d.Query("SELECT name FROM sqlite_master WHERE type='table'")
	require.NoError(t, err)
	defer rows.Close()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables[name] = true
	}
	require.NoError(t, rows.Err())

	assert.True(t, tables["users"], "users 表应存在")
	assert.True(t, tables["media_files"], "media_files 表应存在")
}

func TestInitSchema_Idempotent(t *testing.T) {
	d, err := Open("file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	err = InitSchema(d)
	require.NoError(t, err)

	// 第二次调用不应报错
	err = InitSchema(d)
	assert.NoError(t, err)
}

func TestOpen_WALMode(t *testing.T) {
	d, err := Open("file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	var journalMode string
	err = d.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode)
}

