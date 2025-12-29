package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

var ErrInsufficientCredits = errors.New("insufficient credits")

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		dsn = buildPostgresDSNFromEnv()
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &PostgresStore{pool: pool}
	if err := s.runMigrations(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func buildPostgresDSNFromEnv() string {
	host := strings.TrimSpace(os.Getenv("POSTGRES_HOST"))
	if host == "" {
		host = "localhost"
	}
	port := strings.TrimSpace(os.Getenv("POSTGRES_PORT"))
	if port == "" {
		port = "5432"
	}
	db := strings.TrimSpace(os.Getenv("POSTGRES_DB"))
	if db == "" {
		db = "bot_converter"
	}
	user := strings.TrimSpace(os.Getenv("POSTGRES_USER"))
	if user == "" {
		user = "bot_converter"
	}
	pass := os.Getenv("POSTGRES_PASSWORD")
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", urlEscape(user), urlEscape(pass), host, port, db)
}

func urlEscape(s string) string {
	r := strings.NewReplacer(
		"%", "%25",
		":", "%3A",
		"/", "%2F",
		"@", "%40",
		"?", "%3F",
		"#", "%23",
		"[", "%5B",
		"]", "%5D",
	)
	return r.Replace(s)
}

func (s *PostgresStore) runMigrations(ctx context.Context) error {
	db := stdlib.OpenDB(*s.pool.Config().ConnConfig)
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations")
}

func (s *PostgresStore) UpsertUser(user types.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `
INSERT INTO users (user_id, chat_id, username, first_name, last_name)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE SET
  chat_id = EXCLUDED.chat_id,
  username = EXCLUDED.username,
  first_name = EXCLUDED.first_name,
  last_name = EXCLUDED.last_name,
  updated_at = NOW();
`, user.UserID, user.ChatID, strings.TrimSpace(user.Username), strings.TrimSpace(user.FirstName), strings.TrimSpace(user.LastName))
	return err
}

func (s *PostgresStore) GetUser(userID int64) (*types.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var u types.User
	err := s.pool.QueryRow(ctx, `
SELECT user_id, chat_id, username, first_name, last_name, created_at, updated_at
FROM users
WHERE user_id = $1
`, userID).Scan(&u.UserID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresStore) UpsertSubscription(sub types.Subscription) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `
INSERT INTO subscriptions (user_id, plan, status, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE SET
  plan = EXCLUDED.plan,
  status = EXCLUDED.status,
  expires_at = EXCLUDED.expires_at,
  updated_at = NOW();
`, sub.UserID, strings.TrimSpace(sub.Plan), strings.TrimSpace(sub.Status), sub.ExpiresAt)
	return err
}

func (s *PostgresStore) GetSubscription(userID int64) (*types.Subscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var sub types.Subscription
	err := s.pool.QueryRow(ctx, `
SELECT user_id, plan, status, expires_at, created_at, updated_at
FROM subscriptions
WHERE user_id = $1
`, userID).Scan(&sub.UserID, &sub.Plan, &sub.Status, &sub.ExpiresAt, &sub.CreatedAt, &sub.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *PostgresStore) IsUnlimited(userID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ok bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1
  FROM subscriptions
  WHERE user_id = $1
    AND status = 'active'
    AND plan = 'unlimited'
    AND (expires_at IS NULL OR expires_at > NOW())
)
`, userID).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (s *PostgresStore) RecordPayment(p types.Payment) (inserted bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.pool.Exec(ctx, `
INSERT INTO payments (user_id, provider, currency, total_amount, invoice_payload, telegram_payment_charge_id, provider_payment_charge_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (telegram_payment_charge_id) DO NOTHING
`, p.UserID, strings.TrimSpace(p.Provider), strings.TrimSpace(p.Currency), p.TotalAmount, strings.TrimSpace(p.InvoicePayload), strings.TrimSpace(p.TelegramPaymentCharge), strings.TrimSpace(p.ProviderPaymentCharge))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *PostgresStore) ActivateOrExtendUnlimited(userID int64, duration time.Duration) (*types.Subscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	var currentExpires *time.Time
	err = tx.QueryRow(ctx, `
SELECT expires_at
FROM subscriptions
WHERE user_id = $1
FOR UPDATE
`, userID).Scan(&currentExpires)
	if err != nil {
		currentExpires = nil
	}

	base := now
	if currentExpires != nil && currentExpires.After(base) {
		base = *currentExpires
	}
	newExpires := base.Add(duration)

	_, err = tx.Exec(ctx, `
INSERT INTO subscriptions (user_id, plan, status, expires_at)
VALUES ($1, 'unlimited', 'active', $2)
ON CONFLICT (user_id) DO UPDATE SET
  plan = 'unlimited',
  status = 'active',
  expires_at = EXCLUDED.expires_at,
  updated_at = NOW()
`, userID, newExpires)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	sub := &types.Subscription{
		UserID:    userID,
		Plan:      "unlimited",
		Status:    "active",
		ExpiresAt: &newExpires,
		UpdatedAt: now,
	}
	return sub, nil
}

func (s *PostgresStore) GetOrResetBalance(userID int64) (int, error) {
	remaining, _, err := s.Consume(userID, 0)
	return remaining, err
}

func nextResetUTC(now time.Time) time.Time {
	now = now.UTC()
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
}

func (s *PostgresStore) Consume(userID int64, credits int) (remaining int, unlimited bool, err error) {
	if credits < 0 {
		credits = 0
	}
	unlimited, err = s.IsUnlimited(userID)
	if err != nil {
		return 0, false, err
	}
	if unlimited {
		return 0, true, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	now := time.Now().UTC()
	resetAt := nextResetUTC(now)
	_, err = tx.Exec(ctx, `
INSERT INTO user_credits (user_id, balance, reset_at)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO NOTHING
`, userID, 50, resetAt)
	if err != nil {
		return 0, false, err
	}

	var balance int
	var currentReset time.Time
	err = tx.QueryRow(ctx, `
SELECT balance, reset_at
FROM user_credits
WHERE user_id = $1
FOR UPDATE
`, userID).Scan(&balance, &currentReset)
	if err != nil {
		return 0, false, err
	}

	if !currentReset.After(now) {
		balance = 50
		currentReset = resetAt
		_, err = tx.Exec(ctx, `
UPDATE user_credits
SET balance = $2, reset_at = $3, updated_at = NOW()
WHERE user_id = $1
`, userID, balance, currentReset)
		if err != nil {
			return 0, false, err
		}
	}

	if credits > 0 {
		if balance < credits {
			return balance, false, ErrInsufficientCredits
		}
		balance = balance - credits
		_, err = tx.Exec(ctx, `
UPDATE user_credits
SET balance = $2, updated_at = NOW()
WHERE user_id = $1
`, userID, balance)
		if err != nil {
			return 0, false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return balance, false, nil
}
