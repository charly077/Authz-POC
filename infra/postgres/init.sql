-- keycloak database is already created by POSTGRES_DB env var
-- Only create the openfga database
SELECT 'CREATE DATABASE openfga'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'openfga')\gexec
