-- +goose Up
CREATE TABLE IF NOT EXISTS users (
  id          BIGSERIAL PRIMARY KEY,
  email       VARCHAR(255) NOT NULL UNIQUE,
  password    TEXT,
  full_name   TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS users;
