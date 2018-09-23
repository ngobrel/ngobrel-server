#!/bin/bash
set -e

echo "Setting up DB"
. /deploy/settings.sh
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
	CREATE USER "$DB_USER";
    ALTER USER "$DB_USER" WITH PASSWORD '$DB_PASS';
	CREATE DATABASE "$DB_NAME";
	GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO "$DB_USER";
EOSQL