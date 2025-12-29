-- +goose Up
CREATE TABLE IF NOT EXISTS payments (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  currency TEXT NOT NULL,
  total_amount BIGINT NOT NULL,
  invoice_payload TEXT NOT NULL,
  telegram_payment_charge_id TEXT NOT NULL UNIQUE,
  provider_payment_charge_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS payments;


