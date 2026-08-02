-- Creates a second database and grants the test user access to it. The
-- mysql image's MYSQL_DATABASE/MYSQL_USER env vars (see
-- docker-compose.test.yml) only grant privileges on that one database, so
-- exercising cross-database behavior (see #20 —
-- internal/engine/mysql/integration_test.go's
-- TestIntegrationMySQLQueryExecuteExplainTargetSelectedDatabase) needs a
-- second, explicitly granted database. Files in this directory run
-- automatically on first container start via
-- /docker-entrypoint-initdb.d.
CREATE DATABASE IF NOT EXISTS dbhelm_test2;
GRANT ALL PRIVILEGES ON dbhelm_test2.* TO 'dbhelm'@'%';
FLUSH PRIVILEGES;
