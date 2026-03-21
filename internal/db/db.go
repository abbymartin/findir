package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS tracked_directories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL UNIQUE,
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS indexed_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    directory_id INTEGER NOT NULL REFERENCES tracked_directories(id),
    path TEXT NOT NULL UNIQUE,
    file_hash TEXT NOT NULL,
    file_size INTEGER,
    modified_at TIMESTAMP,
    indexed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS embeddings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL REFERENCES indexed_files(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    chunk_text TEXT NOT NULL,
    embedding BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

type DB struct {
	conn *sql.DB
}

type TrackedDirectory struct {
	ID      int64
	Path    string
	AddedAt time.Time
}

type IndexedFile struct {
	ID          int64
	DirectoryID int64
	Path        string
	FileHash    string
	FileSize    int64
	ModifiedAt  time.Time
	IndexedAt   time.Time
}

func InitDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) AddTrackedDirectory(path string) (int64, error) {
	result, err := d.conn.Exec("INSERT INTO tracked_directories (path) VALUES (?)", path)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (d *DB) GetTrackedDirectories() ([]TrackedDirectory, error) {
	rows, err := d.conn.Query("SELECT id, path, added_at FROM tracked_directories")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirs []TrackedDirectory
	for rows.Next() {
		var dir TrackedDirectory
		if err := rows.Scan(&dir.ID, &dir.Path, &dir.AddedAt); err != nil {
			return nil, err
		}
		dirs = append(dirs, dir)
	}
	return dirs, rows.Err()
}

func (d *DB) GetIndexedFiles(directoryID int64) ([]IndexedFile, error) {
	rows, err := d.conn.Query(
		"SELECT id, directory_id, path, file_hash, file_size, modified_at, indexed_at FROM indexed_files WHERE directory_id = ?",
		directoryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []IndexedFile
	for rows.Next() {
		var f IndexedFile
		if err := rows.Scan(&f.ID, &f.DirectoryID, &f.Path, &f.FileHash, &f.FileSize, &f.ModifiedAt, &f.IndexedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (d *DB) FileIsIndexed(path string, hash string) (bool, error) {
	var count int
	err := d.conn.QueryRow(
		"SELECT COUNT(*) FROM indexed_files WHERE path = ? AND file_hash = ?",
		path, hash,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
