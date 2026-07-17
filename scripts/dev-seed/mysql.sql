-- Sample schema + data for manually exercising the desktop app's SQL
-- surfaces (Tables, Query, FK navigation/relationship inspector, Schema
-- Diff, AI mock-data/codegen) against MySQL.
--
-- Usage:
--   docker compose -f docker-compose.test.yml up -d mysql
--   mysql -h 127.0.0.1 -P 53306 -u mongobak -pmongobak mongobak_test < scripts/dev-seed/mysql.sql
--   mongobak connection add mysql-dev --engine mysql \
--     --uri "mongobak:mongobak@tcp(127.0.0.1:53306)/mongobak_test"

DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
  id INT AUTO_INCREMENT PRIMARY KEY,
  email VARCHAR(255) NOT NULL UNIQUE,
  full_name VARCHAR(255) NOT NULL,
  metadata JSON,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE orders (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  total DECIMAL(10, 2) NOT NULL,
  status VARCHAR(50) NOT NULL DEFAULT 'pending',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE order_items (
  id INT AUTO_INCREMENT PRIMARY KEY,
  order_id INT NOT NULL,
  sku VARCHAR(100) NOT NULL,
  quantity INT NOT NULL,
  unit_price DECIMAL(10, 2) NOT NULL,
  FOREIGN KEY (order_id) REFERENCES orders(id)
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
