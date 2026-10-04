package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Verification struct {
	Verified    bool
	Question    string
	Answer      int
	Attempts    int
	ExpiresAt   time.Time
	LockedUntil time.Time
}

type Ticket struct {
	ID        int64
	UserID    int64
	FirstName string
	ThreadID  int
	Status    string
	CreatedAt time.Time
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &Store{db: db}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
    user_id INTEGER PRIMARY KEY,
    banned INTEGER NOT NULL DEFAULT 0,
    verified INTEGER NOT NULL DEFAULT 0,
    question TEXT NOT NULL DEFAULT '',
    answer INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0,
    locked_until INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tickets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    first_name TEXT NOT NULL,
    thread_id INTEGER NOT NULL DEFAULT 0 UNIQUE,
    status TEXT NOT NULL DEFAULT 'open',
    created_at INTEGER NOT NULL,
    closed_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS tickets_user_status ON tickets(user_id, status);
CREATE TABLE IF NOT EXISTS ticket_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id INTEGER NOT NULL,
    direction TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL
);`)
	if err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	// Keep existing personal deployments upgradeable after adding staff bans.
	if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN banned INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migrate user bans: %w", err)
	}
	return nil
}

func (s *Store) GetVerification(userID int64) (Verification, error) {
	var v Verification
	var verified int
	var expiresAt, lockedUntil int64
	err := s.db.QueryRow(`SELECT verified, question, answer, attempts, expires_at, locked_until FROM users WHERE user_id = ?`, userID).
		Scan(&verified, &v.Question, &v.Answer, &v.Attempts, &expiresAt, &lockedUntil)
	if err == sql.ErrNoRows {
		return Verification{}, nil
	}
	if err != nil {
		return Verification{}, err
	}
	v.Verified = verified == 1
	v.ExpiresAt = time.Unix(expiresAt, 0)
	v.LockedUntil = time.Unix(lockedUntil, 0)
	return v, nil
}

func (s *Store) IsBanned(userID int64) (bool, error) {
	var banned int
	err := s.db.QueryRow(`SELECT banned FROM users WHERE user_id=?`, userID).Scan(&banned)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return banned == 1, err
}

func (s *Store) BanUser(userID int64) error {
	_, err := s.db.Exec(`INSERT INTO users(user_id, banned, updated_at) VALUES(?, 1, ?)
ON CONFLICT(user_id) DO UPDATE SET banned=1, updated_at=excluded.updated_at`, userID, time.Now().Unix())
	return err
}

func (s *Store) SaveChallenge(userID int64, question string, answer int, expiresAt time.Time) error {
	_, err := s.db.Exec(`INSERT INTO users(user_id, verified, question, answer, attempts, expires_at, locked_until, updated_at)
VALUES(?, 0, ?, ?, 0, ?, 0, ?)
ON CONFLICT(user_id) DO UPDATE SET verified=0, question=excluded.question, answer=excluded.answer,
attempts=0, expires_at=excluded.expires_at, locked_until=0, updated_at=excluded.updated_at`,
		userID, question, answer, expiresAt.Unix(), time.Now().Unix())
	return err
}

func (s *Store) MarkVerified(userID int64) error {
	_, err := s.db.Exec(`UPDATE users SET verified=1, question='', answer=0, attempts=0, expires_at=0, locked_until=0, updated_at=? WHERE user_id=?`, time.Now().Unix(), userID)
	return err
}

func (s *Store) RecordFailure(userID int64, attempts int, lockedUntil time.Time) error {
	_, err := s.db.Exec(`UPDATE users SET attempts=?, locked_until=?, updated_at=? WHERE user_id=?`, attempts, lockedUntil.Unix(), time.Now().Unix(), userID)
	return err
}

func (s *Store) OpenTicket(userID int64) (Ticket, error) {
	var t Ticket
	var createdAt int64
	err := s.db.QueryRow(`SELECT id, user_id, first_name, thread_id, status, created_at FROM tickets WHERE user_id=? AND status='open' ORDER BY id DESC LIMIT 1`, userID).
		Scan(&t.ID, &t.UserID, &t.FirstName, &t.ThreadID, &t.Status, &createdAt)
	if err == sql.ErrNoRows {
		return Ticket{}, nil
	}
	if err != nil {
		return Ticket{}, err
	}
	t.CreatedAt = time.Unix(createdAt, 0)
	return t, nil
}

func (s *Store) CreateTicket(userID int64, firstName string) (Ticket, error) {
	result, err := s.db.Exec(`INSERT INTO tickets(user_id, first_name, created_at) VALUES(?, ?, ?)`, userID, firstName, time.Now().Unix())
	if err != nil {
		return Ticket{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Ticket{}, err
	}
	return Ticket{ID: id, UserID: userID, FirstName: firstName, Status: "open", CreatedAt: time.Now()}, nil
}

func (s *Store) SetThreadID(ticketID int64, threadID int) error {
	_, err := s.db.Exec(`UPDATE tickets SET thread_id=? WHERE id=?`, threadID, ticketID)
	return err
}

func (s *Store) TicketByThread(threadID int) (Ticket, error) {
	var t Ticket
	var createdAt int64
	err := s.db.QueryRow(`SELECT id, user_id, first_name, thread_id, status, created_at FROM tickets WHERE thread_id=?`, threadID).
		Scan(&t.ID, &t.UserID, &t.FirstName, &t.ThreadID, &t.Status, &createdAt)
	if err == sql.ErrNoRows {
		return Ticket{}, nil
	}
	if err != nil {
		return Ticket{}, err
	}
	t.CreatedAt = time.Unix(createdAt, 0)
	return t, nil
}

func (s *Store) CloseTicket(ticketID int64) error {
	_, err := s.db.Exec(`UPDATE tickets SET status='closed', closed_at=? WHERE id=?`, time.Now().Unix(), ticketID)
	return err
}

func (s *Store) AddMessage(ticketID int64, direction, body string) error {
	_, err := s.db.Exec(`INSERT INTO ticket_messages(ticket_id, direction, body, created_at) VALUES(?, ?, ?, ?)`, ticketID, direction, body, time.Now().Unix())
	return err
}
