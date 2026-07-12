CREATE TABLE users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE library_paths (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL,
	type TEXT NOT NULL DEFAULT 'local',
	label TEXT,
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX idx_library_paths_path ON library_paths(path);

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

CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT,
	updated_at DATETIME
);

CREATE TABLE `albums` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`description` text,`cover_media_id` integer DEFAULT 0,`created_at` datetime,`updated_at` datetime);

CREATE TABLE `album_items` (`id` integer PRIMARY KEY AUTOINCREMENT,`album_id` integer NOT NULL,`media_id` integer NOT NULL,`added_at` datetime);
CREATE UNIQUE INDEX idx_album_items_album_media ON album_items(album_id, media_id);

CREATE TABLE `tags` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`created_at` datetime);
CREATE UNIQUE INDEX idx_tags_name ON tags(name);

CREATE TABLE `tag_mappings` (`id` integer PRIMARY KEY AUTOINCREMENT,`tag_id` integer NOT NULL,`media_id` integer NOT NULL);
CREATE UNIQUE INDEX idx_tag_mappings_tag_media ON tag_mappings(tag_id, media_id);

CREATE TABLE `scan_tasks` (`id` integer PRIMARY KEY AUTOINCREMENT,`library_id` integer NOT NULL,`scan_type` text NOT NULL DEFAULT "full",`status` text NOT NULL DEFAULT "pending",`scanned_files` integer DEFAULT 0,`total_files` integer DEFAULT 0,`error` text,`created_at` datetime,`started_at` datetime,`completed_at` datetime);
CREATE INDEX idx_scan_tasks_library_id ON scan_tasks(library_id);
CREATE INDEX idx_scan_tasks_status ON scan_tasks(status);

CREATE TABLE `transcode_presets` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`codec` text NOT NULL,`width` integer DEFAULT 0,`height` integer DEFAULT 0,`created_at` datetime,`updated_at` datetime);

CREATE TABLE `transcode_tasks` (`id` integer PRIMARY KEY AUTOINCREMENT,`media_id` integer NOT NULL,`preset_id` integer NOT NULL,`codec` text NOT NULL,`width` integer DEFAULT 0,`height` integer DEFAULT 0,`status` text NOT NULL DEFAULT "pending",`error` text,`created_at` datetime,`started_at` datetime,`completed_at` datetime);
CREATE INDEX idx_transcode_tasks_media_id ON transcode_tasks(media_id);
CREATE INDEX idx_transcode_tasks_preset_id ON transcode_tasks(preset_id);
CREATE INDEX idx_transcode_tasks_status ON transcode_tasks(status);

CREATE TABLE `shares` (`token` text,`resource_type` text NOT NULL,`resource_id` integer NOT NULL,`expires_at` datetime,`password_hash` text DEFAULT "",`max_uses` integer DEFAULT 0,`used_count` integer DEFAULT 0,`created_at` datetime,PRIMARY KEY (`token`));
CREATE INDEX idx_shares_resource_id ON shares(resource_id);

CREATE TABLE `media_health_issues` (`id` integer PRIMARY KEY AUTOINCREMENT,`media_id` integer NOT NULL,`issue_type` text NOT NULL,`detail` text,`checked_at` datetime);
CREATE INDEX idx_media_health_issues_media_id ON media_health_issues(media_id);
CREATE INDEX idx_media_health_issues_issue_type ON media_health_issues(issue_type);

CREATE TABLE media_extensions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	library_id INTEGER NOT NULL,
	extension TEXT NOT NULL,
	type TEXT NOT NULL,
	is_built_in INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX idx_media_extensions_library_extension ON media_extensions(library_id, extension);

INSERT INTO users(id, username, password_hash, created_at) VALUES
	(1, 'admin', 'legacy-admin-hash', '2025-02-01T08:00:00Z'),
	(2, 'viewer', 'legacy-viewer-hash', '2025-02-02T08:00:00Z');

INSERT INTO library_paths(id, path, type, label, enabled, created_at) VALUES
	(1, 'D:/Archive/Movies', 'local', '电影库', 1, '2025-03-01T08:00:00Z'),
	(2, 'D:/Archive/Family', 'local', '家庭影像', 1, '2025-03-02T08:00:00Z'),
	(3, '//nas/photos', 'smb', '照片库', 0, '2025-03-03T08:00:00Z');

INSERT INTO media_files(
	id, library_id, file_path, file_name, file_size, format, video_codec, audio_codec,
	duration, width, height, bitrate, subtitle_tracks, added_at, modified_at
) VALUES
	(11, 1, 'D:/Archive/Movies/alpha.mp4', 'alpha.mp4', 1048576, 'mp4', 'h264', 'aac', 90.5, 1920, 1080, 4500000, '[{"language":"zh"}]', '2025-03-10T08:00:00Z', '2025-03-10T09:00:00Z'),
	(12, 1, 'D:/Archive/Movies/beta.mkv', 'beta.mkv', 2097152, 'mkv', 'hevc', 'aac', 180.25, 3840, 2160, 12000000, '[]', '2025-03-11T08:00:00Z', '2025-03-11T09:00:00Z'),
	(21, 2, 'D:/Archive/Family/birthday.mov', 'birthday.mov', 524288, 'mov', 'h264', 'pcm_s16le', 45.0, 1280, 720, 2500000, NULL, '2025-03-12T08:00:00Z', '2025-03-12T09:00:00Z'),
	(31, 3, '//nas/photos/trip.jpg', 'trip.jpg', 262144, 'jpg', '', '', 0, 6000, 4000, 0, NULL, '2025-03-13T08:00:00Z', '2025-03-13T09:00:00Z');

INSERT INTO settings(key, value, updated_at) VALUES
	('scan_interval', '7200', '2025-04-01T08:00:00Z'),
	('ffmpeg_path', 'D:/Tools/ffmpeg.exe', '2025-04-02T08:00:00Z'),
	('network_proxy', 'http://127.0.0.1:7890', '2025-04-03T08:00:00Z'),
	('debug_log', 'false', '2025-04-04T08:00:00Z');

INSERT INTO albums(id, name, description, cover_media_id, created_at, updated_at) VALUES
	(41, '年度精选', '旧版相册说明', 11, '2025-05-01T08:00:00Z', '2025-05-02T08:00:00Z'),
	(42, '家庭记录', '', 21, '2025-05-03T08:00:00Z', '2025-05-04T08:00:00Z');

INSERT INTO album_items(id, album_id, media_id, added_at) VALUES
	(51, 41, 11, '2025-05-05T08:00:00Z'),
	(52, 41, 12, '2025-05-06T08:00:00Z'),
	(53, 42, 21, '2025-05-07T08:00:00Z');

INSERT INTO tags(id, name, created_at) VALUES
	(61, '收藏', '2025-05-08T08:00:00Z'),
	(62, '家庭', '2025-05-09T08:00:00Z');
INSERT INTO tag_mappings(id, tag_id, media_id) VALUES
	(71, 61, 11),
	(72, 62, 21);

INSERT INTO scan_tasks(id, library_id, scan_type, status, scanned_files, total_files, error, created_at, started_at, completed_at) VALUES
	(101, 1, 'full', 'completed', 20, 20, '', '2025-06-01T08:00:00Z', '2025-06-01T08:01:00Z', '2025-06-01T08:10:00Z'),
	(102, 2, 'incremental', 'error', 3, 10, '目录暂不可用', '2025-06-02T08:00:00Z', '2025-06-02T08:01:00Z', '2025-06-02T08:02:00Z'),
	(103, 3, 'full', 'running', 4, 8, '', '2025-06-03T08:00:00Z', '2025-06-03T08:01:00Z', NULL),
	(104, 1, 'incremental', 'pending', 0, 0, '', '2025-06-04T08:00:00Z', NULL, NULL);

INSERT INTO transcode_presets(id, name, codec, width, height, created_at, updated_at) VALUES
	(81, '兼容 1080p', 'h264', 1920, 1080, '2025-06-05T08:00:00Z', '2025-06-05T08:00:00Z'),
	(82, '省流 720p', 'h265', 1280, 720, '2025-06-06T08:00:00Z', '2025-06-06T08:00:00Z');

INSERT INTO transcode_tasks(id, media_id, preset_id, codec, width, height, status, error, created_at, started_at, completed_at) VALUES
	(201, 11, 81, 'h264', 1920, 1080, 'completed', '', '2025-06-07T08:00:00Z', '2025-06-07T08:01:00Z', '2025-06-07T08:20:00Z'),
	(202, 12, 82, 'h265', 1280, 720, 'error', '编码器不可用', '2025-06-08T08:00:00Z', '2025-06-08T08:01:00Z', '2025-06-08T08:02:00Z'),
	(203, 21, 81, 'h264', 0, 0, 'running', '', '2025-06-09T08:00:00Z', '2025-06-09T08:01:00Z', NULL),
	(204, 31, 82, 'h265', 1280, 720, 'pending', '', '2025-06-10T08:00:00Z', NULL, NULL);

INSERT INTO shares(token, resource_type, resource_id, expires_at, password_hash, max_uses, used_count, created_at) VALUES
	('legacy-media-token', 'media', 11, NULL, '', 0, 7, '2025-06-11T08:00:00Z'),
	('legacy-album-token', 'album', 41, '2027-01-01T00:00:00Z', 'legacy-share-hash', 20, 3, '2025-06-12T08:00:00Z');

INSERT INTO media_health_issues(id, media_id, issue_type, detail, checked_at) VALUES
	(301, 12, 'no_thumbnail', '旧版缩略图缺失', '2025-06-13T08:00:00Z'),
	(302, 31, 'missing', 'NAS 暂时离线', '2025-06-14T08:00:00Z');

INSERT INTO media_extensions(id, library_id, extension, type, is_built_in, created_at) VALUES
	(401, 1, '.mp4', 'video', 1, '2025-06-15T08:00:00Z'),
	(402, 3, '.jpg', 'image', 1, '2025-06-16T08:00:00Z'),
	(403, 2, '.mjpeg', 'video', 0, '2025-06-17T08:00:00Z');
