package coord

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// APIKey represents a stored API key row.
type APIKey struct {
	ID        int64
	UserID    int64
	KeyHash   string
	KeyPrefix string
	Scope     string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// DonorStats represents persistent donor statistics.
type DonorStats struct {
	UserID            int64
	TotalRequests     int64
	TotalTokens       int64
	TotalUptimeSec    int64
	LastSeenAt        *time.Time
}

// Store manages persistent state via SQLite.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) the SQLite database and runs migrations.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Performance pragmas.
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA foreign_keys=ON")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		github_id INTEGER UNIQUE NOT NULL,
		github_login TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		key_hash TEXT UNIQUE NOT NULL,
		key_prefix TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT 'consumer',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		revoked_at TEXT
	);

	CREATE TABLE IF NOT EXISTS donor_stats (
		user_id INTEGER PRIMARY KEY REFERENCES users(id),
		total_requests INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		total_uptime_seconds INTEGER NOT NULL DEFAULT 0,
		last_seen_at TEXT
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		expires_at TEXT NOT NULL
	);
	`
	_, err := db.Exec(schema)
	return err
}

// UpsertUser inserts a user if not present or updates login, returns the user ID.
func (s *Store) UpsertUser(githubID int64, login string) (int64, error) {
	_, err := s.db.Exec(
		`INSERT INTO users (github_id, github_login) VALUES (?, ?)
		 ON CONFLICT(github_id) DO UPDATE SET github_login = excluded.github_login`,
		githubID, login,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert user: %w", err)
	}

	var id int64
	err = s.db.QueryRow(`SELECT id FROM users WHERE github_id = ?`, githubID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("select user: %w", err)
	}
	return id, nil
}

// CreateKey generates a new API key, stores its SHA-256 hash, and returns the raw key.
func (s *Store) CreateKey(userID int64, scope string) (rawKey string, keyID int64, err error) {
	raw, err := generateAPIKey()
	if err != nil {
		return "", 0, fmt.Errorf("generate key: %w", err)
	}

	hash := hashKey(raw)
	prefix := raw[:8] // first 8 chars per §5.6 SPEC

	res, err := s.db.Exec(
		`INSERT INTO api_keys (user_id, key_hash, key_prefix, scope) VALUES (?, ?, ?, ?)`,
		userID, hash, prefix, scope,
	)
	if err != nil {
		return "", 0, fmt.Errorf("insert key: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return "", 0, fmt.Errorf("last insert id: %w", err)
	}
	keyID = id
	return raw, keyID, nil
}

// ListKeys returns all non-revoked API keys for a user.
func (s *Store) ListKeys(userID int64) ([]APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, key_hash, key_prefix, scope, created_at, revoked_at
		 FROM api_keys WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var createdStr string
		var revokedStr *string
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.KeyPrefix, &k.Scope, &createdStr, &revokedStr); err != nil {
			return nil, fmt.Errorf("scan key: %w", err)
		}
		k.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if revokedStr != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *revokedStr)
			k.RevokedAt = &t
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// CountKeys returns the number of non-revoked API keys for a user.
func (s *Store) CountKeys(userID int64) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL`,
		userID,
	).Scan(&count)
	return count, err
}

// RevokeKey marks a key as revoked. Only the owner can revoke.
func (s *Store) RevokeKey(userID, keyID int64) error {
	res, err := s.db.Exec(
		`UPDATE api_keys SET revoked_at = datetime('now')
		 WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		keyID, userID,
	)
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("key not found or already revoked")
	}
	return nil
}

// FindKeyByHash looks up an API key by its SHA-256 hash.
// Returns nil if not found or revoked.
func (s *Store) FindKeyByHash(hash string) (*APIKey, error) {
	var k APIKey
	var createdStr string
	var revokedStr *string
	err := s.db.QueryRow(
		`SELECT id, user_id, key_hash, key_prefix, scope, created_at, revoked_at
		 FROM api_keys WHERE key_hash = ?`, hash,
	).Scan(&k.ID, &k.UserID, &k.KeyHash, &k.KeyPrefix, &k.Scope, &createdStr, &revokedStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find key: %w", err)
	}
	k.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	if revokedStr != nil {
		return nil, nil // revoked == not found for auth purposes
	}
	return &k, nil
}

// GetDonorStats returns persistent donor statistics for a user.
func (s *Store) GetDonorStats(userID int64) (*DonorStats, error) {
	var ds DonorStats
	var lastSeen *string
	err := s.db.QueryRow(
		`SELECT user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at
		 FROM donor_stats WHERE user_id = ?`, userID,
	).Scan(&ds.UserID, &ds.TotalRequests, &ds.TotalTokens, &ds.TotalUptimeSec, &lastSeen)
	if err == sql.ErrNoRows {
		return &DonorStats{UserID: userID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get donor stats: %w", err)
	}
	if lastSeen != nil {
		t, _ := time.Parse("2006-01-02 15:04:05", *lastSeen)
		ds.LastSeenAt = &t
	}
	return &ds, nil
}

// ListAllDonorStats returns all donor statistics rows, ordered by total tokens descending.
func (s *Store) ListAllDonorStats() ([]DonorStats, error) {
	rows, err := s.db.Query(
		`SELECT user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at
		 FROM donor_stats ORDER BY total_tokens DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all donor stats: %w", err)
	}
	defer rows.Close()

	var result []DonorStats
	for rows.Next() {
		var ds DonorStats
		var lastSeen *string
		if err := rows.Scan(&ds.UserID, &ds.TotalRequests, &ds.TotalTokens, &ds.TotalUptimeSec, &lastSeen); err != nil {
			return nil, fmt.Errorf("list all donor stats: %w", err)
		}
		if lastSeen != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *lastSeen)
			ds.LastSeenAt = &t
		}
		result = append(result, ds)
	}
	return result, rows.Err()
}

// UpdateDonorStats increments persistent donor counters.
func (s *Store) UpdateDonorStats(userID int64, requests, tokens, uptimeSec int64) error {
	_, err := s.db.Exec(
		`INSERT INTO donor_stats (user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at)
		 VALUES (?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(user_id) DO UPDATE SET
		   total_requests = total_requests + excluded.total_requests,
		   total_tokens = total_tokens + excluded.total_tokens,
		   total_uptime_seconds = total_uptime_seconds + excluded.total_uptime_seconds,
		   last_seen_at = excluded.last_seen_at`,
		userID, requests, tokens, uptimeSec,
	)
	if err != nil {
		return fmt.Errorf("update donor stats: %w", err)
	}
	return nil
}

// --- Session management ---

// CreateSession creates a session for the given user, valid for 24 hours.
func (s *Store) CreateSession(userID int64) (token string, err error) {
	token, err = generateSessionToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(
		`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, datetime('now', '+24 hours'))`,
		token, userID,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// ValidateSession returns the user ID for a valid session, or 0 if expired/invalid.
func (s *Store) ValidateSession(token string) (int64, error) {
	var userID int64
	err := s.db.QueryRow(
		`SELECT user_id FROM sessions WHERE token = ? AND expires_at > datetime('now')`,
		token,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("validate session: %w", err)
	}
	return userID, nil
}

// DeleteSession removes a session (logout).
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// GetUserByID returns the github login for a user ID.
func (s *Store) GetUserByID(userID int64) (githubLogin string, err error) {
	err = s.db.QueryRow(`SELECT github_login FROM users WHERE id = ?`, userID).Scan(&githubLogin)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("user not found")
	}
	return githubLogin, err
}

// --- Helpers ---

func generateAPIKey() (string, error) {
	b := make([]byte, 16) // 32 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "inf_" + hex.EncodeToString(b), nil
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
