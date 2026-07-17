-- Sample schema + data for manually exercising the desktop app's SQL
-- surfaces (Tables, Query, FK navigation/relationship inspector, Schema
-- Diff, AI mock-data/codegen, and — if your Postgres has the PostGIS
-- extension installed — the automatic ST_AsGeoJSON geometry wrapping).
--
-- Usage:
--   docker compose -f docker-compose.test.yml up -d postgres
--   psql "postgres://mongobak:mongobak@localhost:55432/mongobak_test" -f scripts/dev-seed/postgres.sql
--   mongobak connection add pg-dev --engine postgres \
--     --uri "postgres://mongobak:mongobak@localhost:55432/mongobak_test"

DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  full_name TEXT NOT NULL,
  metadata JSONB,
  created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE orders (
  id SERIAL PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  total NUMERIC(10, 2) NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE order_items (
  id SERIAL PRIMARY KEY,
  order_id INTEGER NOT NULL REFERENCES orders(id),
  sku TEXT NOT NULL,
  quantity INTEGER NOT NULL,
  unit_price NUMERIC(10, 2) NOT NULL
);

INSERT INTO users (email, full_name, metadata) VALUES
  ('ada@example.com', 'Ada Lovelace', '{"plan": "pro", "referrals": 3}'),
  ('grace@example.com', 'Grace Hopper', '{"plan": "free", "referrals": 0}'),
  ('alan@example.com', 'Alan Turing', '{"plan": "pro", "referrals": 12}');

INSERT INTO orders (user_id, total, status) VALUES
  (1, 129.99, 'shipped'),
  (1, 42.50, 'pending'),
  (2, 19.99, 'delivered'),
  (3, 310.00, 'shipped');

INSERT INTO order_items (order_id, sku, quantity, unit_price) VALUES
  (1, 'WIDGET-A', 2, 49.99), (1, 'WIDGET-B', 1, 30.01),
  (2, 'GADGET-X', 1, 42.50),
  (3, 'WIDGET-A', 1, 19.99),
  (4, 'GADGET-X', 5, 62.00);

-- Uncomment if your Postgres instance has PostGIS installed (the plain
-- `postgres:16` image in docker-compose.test.yml does not — use
-- `postgis/postgis:16-3.4` instead if you want to try this):
--
-- CREATE EXTENSION IF NOT EXISTS postgis;
-- CREATE TABLE job_sites (
--   id SERIAL PRIMARY KEY,
--   name TEXT NOT NULL,
--   geofence geometry(Polygon, 4326)
-- );
-- INSERT INTO job_sites (name, geofence) VALUES
--   ('Downtown site', ST_GeomFromText(
--     'POLYGON((-122.084 37.4219, -122.083 37.4219, -122.083 37.4225, -122.084 37.4225, -122.084 37.4219))', 4326));
