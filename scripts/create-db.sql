-- One-time setup on a native PostgreSQL: creates the moneta role and database.
-- Run as the postgres superuser, passing the password from .env (POSTGRES_PASSWORD):
--   psql -U postgres -v pw=<password> -f scripts/create-db.sql
CREATE ROLE moneta LOGIN PASSWORD :'pw';
CREATE DATABASE moneta OWNER moneta;
