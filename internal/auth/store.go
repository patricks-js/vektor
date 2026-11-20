// Package auth manages the local administrator and opaque SQLite sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"vektor/internal/database"
)

const (
	Username = "admin"

	MinPasswordBytes = 12
	MaxPasswordBytes = 1024
	maxSessions      = 256
	saltBytes        = 16
	keyBytes         = 32

	// Argon2id parameters from the second recommended profile in RFC 9106.
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
)

var (
	ErrAlreadyBootstrapped = errors.New("administrator already exists")
	ErrPasswordLength      = errors.New("password must contain between 12 and 1024 bytes")
	ErrPasswordEncoding    = errors.New("password must be valid UTF-8")
	ErrCredentials         = errors.New("invalid credentials")
	ErrSession             = errors.New("invalid or expired session")
	ErrSessionLimit        = errors.New("active session limit reached")
)

type Session struct {
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
	CSRFToken string    `json:"csrf_token"`
}

type Store struct {
	db     *database.DB
	ttl    time.Duration
	now    func() time.Time
	mu     sync.Mutex
	change chan struct{}
}

func NewStore(db *database.DB, ttl time.Duration) *Store {
	return &Store{db: db, ttl: ttl, now: time.Now, change: make(chan struct{})}
}

func (s *Store) Bootstrap(ctx context.Context, password []byte) error {
	if len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes {
		return ErrPasswordLength
	}
	if !utf8.Valid(password) {
		return ErrPasswordEncoding
	}
	var salt [saltBytes]byte
	rand.Read(salt[:])
	hash := passwordHash(password, salt[:])
	result, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO admin_credentials (id, salt, password_hash) VALUES (1, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, salt[:], hash)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrAlreadyBootstrapped
	}
	return err
}

func passwordHash(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, keyBytes)
}

func (s *Store) Login(ctx context.Context, username string, password []byte) (string, Session, error) {
	if len(password) > MaxPasswordBytes {
		return "", Session{}, ErrCredentials
	}
	var salt, want []byte
	err := s.db.Reader().QueryRowContext(ctx, `SELECT salt, password_hash FROM admin_credentials WHERE id = 1`).Scan(&salt, &want)
	exists := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		salt, want = make([]byte, saltBytes), make([]byte, keyBytes)
	} else if err != nil {
		return "", Session{}, err
	}

	got := passwordHash(password, salt)
	userHash, adminHash := sha256.Sum256([]byte(username)), sha256.Sum256([]byte(Username))
	if subtle.ConstantTimeCompare(got, want)&subtle.ConstantTimeCompare(userHash[:], adminHash[:]) != 1 || !exists {
		return "", Session{}, ErrCredentials
	}

	var secret [keyBytes]byte
	rand.Read(secret[:])
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	digest := sha256.Sum256(secret[:])
	now := s.now()
	expiry := now.Add(s.ttl).UnixMilli()
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return "", Session{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at_ms <= ?`, now.UnixMilli()); err != nil {
		return "", Session{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO admin_sessions (token_hash, expires_at_ms)
		SELECT ?, ? WHERE (SELECT COUNT(*) FROM admin_sessions) < ?
	`, digest[:], expiry, maxSessions)
	if err != nil {
		return "", Session{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", Session{}, err
	}
	if count == 0 {
		return "", Session{}, ErrSessionLimit
	}
	if err := tx.Commit(); err != nil {
		return "", Session{}, err
	}
	return token, session(token, expiry), nil
}

func (s *Store) Authenticate(ctx context.Context, token string) (Session, error) {
	digest, err := tokenHash(token)
	if err != nil {
		return Session{}, err
	}
	var expiry int64
	err = s.db.Reader().QueryRowContext(ctx, `SELECT expires_at_ms FROM admin_sessions WHERE token_hash = ?`, digest[:]).Scan(&expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSession
	}
	if err != nil {
		return Session{}, err
	}
	if expiry <= s.now().UnixMilli() {
		return Session{}, ErrSession
	}
	return session(token, expiry), nil
}

func (s *Store) Revoke(ctx context.Context, token string) error {
	digest, err := tokenHash(token)
	if err != nil {
		return err
	}
	if _, err := s.db.Writer().ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash = ?`, digest[:]); err != nil {
		return err
	}
	s.notifyRevocation()
	return nil
}

func (s *Store) RevokeAll(ctx context.Context) error {
	if _, err := s.db.Writer().ExecContext(ctx, `DELETE FROM admin_sessions`); err != nil {
		return err
	}
	s.notifyRevocation()
	return nil
}

// Revocations returns a channel closed when a session is revoked by this Store.
// Capture it before checking membership so a concurrent logout cannot be missed.
func (s *Store) Revocations() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.change
}

func (s *Store) notifyRevocation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.change)
	s.change = make(chan struct{})
}

func tokenHash(token string) ([keyBytes]byte, error) {
	var secret [keyBytes]byte
	if len(token) != base64.RawURLEncoding.EncodedLen(keyBytes) {
		return secret, ErrSession
	}
	n, err := base64.RawURLEncoding.Strict().Decode(secret[:], []byte(token))
	if err != nil || n != keyBytes {
		return secret, ErrSession
	}
	return sha256.Sum256(secret[:]), nil
}

func session(token string, expiry int64) Session {
	// Domain separation keeps this value independent of the stored token digest.
	csrf := sha256.Sum256([]byte("vektor-csrf-v1\x00" + token))
	return Session{
		Username:  Username,
		ExpiresAt: time.UnixMilli(expiry).UTC(),
		CSRFToken: base64.RawURLEncoding.EncodeToString(csrf[:]),
	}
}
