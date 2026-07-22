CREATE TABLE library_paths (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL,
	type TEXT NOT NULL DEFAULT 'local',
	label TEXT,
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE media_files (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	library_id INTEGER NOT NULL,
	file_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	file_size INTEGER DEFAULT 0,
	format TEXT,
	video_codec TEXT,
	audio_codec TEXT,
	duration REAL DEFAULT 0,
	width INTEGER DEFAULT 0,
	height INTEGER DEFAULT 0,
	bitrate INTEGER DEFAULT 0,
	subtitle_tracks TEXT,
	added_at DATETIME,
	modified_at DATETIME
);

CREATE INDEX idx_media_files_file_path ON media_files(file_path);

CREATE TABLE users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT,
	updated_at DATETIME
);

INSERT INTO library_paths(path, type, label, enabled, created_at)
VALUES ('D:/media', 'local', '旧媒体库', 1, '2026-07-01T00:00:00Z');

INSERT INTO media_files(library_id, file_path, file_name, file_size, format, duration, added_at, modified_at)
VALUES (1, 'D:/media/a.mp4', 'a.mp4', 100, 'mp4', 12.5, '2026-07-02T00:00:00Z', '2026-07-02T00:00:00Z'),
       (1, 'D:/media/b.mkv', 'b.mkv', 200, 'mkv', 22.5, '2026-07-03T00:00:00Z', '2026-07-03T00:00:00Z');

INSERT INTO users(username, password_hash)
VALUES ('admin', 'hash');

INSERT INTO settings(key, value, updated_at)
VALUES ('scan_interval', '3600', '2026-07-01T00:00:00Z');
