package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetrySQLiteBusySnapshotRetriesUntilSuccess(t *testing.T) {
	attempts := 0
	err := RetrySQLiteBusySnapshot(context.Background(), func() error {
		attempts++
		if attempts < 5 {
			return sqlite3.Error{Code: sqlite3.ErrBusy, ExtendedCode: sqlite3.ErrBusySnapshot}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 5, attempts)
}

func TestRetrySQLiteBusySnapshotDoesNotRetryOrdinaryBusy(t *testing.T) {
	attempts := 0
	err := RetrySQLiteBusySnapshot(context.Background(), func() error {
		attempts++
		return sqlite3.Error{Code: sqlite3.ErrBusy}
	})
	require.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestRetrySQLiteBusySnapshotHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := RetrySQLiteBusySnapshot(ctx, func() error {
		attempts++
		cancel()
		return sqlite3.Error{Code: sqlite3.ErrBusy, ExtendedCode: sqlite3.ErrBusySnapshot}
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, attempts)
}

func TestOpen_MemoryDB(t *testing.T) {
	d, err := Open("file::memory:?cache=shared")
	require.NoError(t, err)
	require.NotNil(t, d)
	t.Cleanup(func() { _ = d.Close() })

	err = d.Ping()
	assert.NoError(t, err)
}

func TestOpen_InvalidDSN(t *testing.T) {
	// 指向不存在的子目录，SQLite 无法创建该文件，Open 应在 Exec/Ping 阶段返回错误。
	// （不用 "\x00invalid" 之类字面量：不同平台/驱动对其处理不一致，可能被当作合法文件名创建。）
	badPath := filepath.Join(t.TempDir(), "no_such_dir", "test.db")
	d, err := Open(badPath)
	assert.Error(t, err)
	assert.Nil(t, d)
}

func TestInitSchema(t *testing.T) {
	d, err := Open("file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	err = InitSchema(d)
	require.NoError(t, err)

	rows, err := d.Query("SELECT name FROM sqlite_master WHERE type='table'")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables[name] = true
	}
	require.NoError(t, rows.Err())

	assert.True(t, tables["users"], "users 表应存在")
	assert.True(t, tables["media_files"], "media_files 表应存在")
	assert.True(t, tables["media_extensions"], "media_extensions 表应存在")
}

func TestInitSchema_Idempotent(t *testing.T) {
	d, err := Open("file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	err = InitSchema(d)
	require.NoError(t, err)

	// 第二次调用不应报错
	err = InitSchema(d)
	assert.NoError(t, err)
}

func TestOpen_WALMode(t *testing.T) {
	// WAL 仅适用于文件型数据库；内存库的 journal_mode 恒为 "memory"，
	// 因此用临时文件路径验证生产路径下 Open 是否真正启用了 WAL。
	dbPath := filepath.Join(t.TempDir(), "wal_test.db")
	d, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	var journalMode string
	err = d.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode)
}
