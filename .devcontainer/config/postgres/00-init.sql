-- Database Initialization
-- ======================
-- This script runs when the PostgreSQL container first starts.

-- Enable pgvector extension for vector similarity search
CREATE EXTENSION IF NOT EXISTS vector;

-- Enable additional useful extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";  -- For UUID generation
CREATE EXTENSION IF NOT EXISTS "pg_trgm";    -- For text similarity
CREATE EXTENSION IF NOT EXISTS "btree_gin";  -- For GIN indexes on btree types

-- Verify pgvector is installed
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
        RAISE EXCEPTION 'pgvector extension failed to install';
    END IF;
    RAISE NOTICE 'pgvector extension successfully installed';
END
$$;

-- Create a function to check vector version
CREATE OR REPLACE FUNCTION vector_version()
RETURNS text AS $$
    SELECT extversion FROM pg_extension WHERE extname = 'vector';
$$ LANGUAGE SQL;

-- Display installation info
DO $$
DECLARE
    vec_version text;
BEGIN
    SELECT vector_version() INTO vec_version;
    RAISE NOTICE 'Database initialized with pgvector %', vec_version;
END
$$;

-- Create azimutt database for Azimutt ERD tool
-- (Azimutt stores its own application data here; databases to visualize are added via UI)
CREATE DATABASE azimutt;
DO $$ BEGIN RAISE NOTICE 'Azimutt database created'; END $$;

-- Create Atlas dev database for schema diffing
-- Used instead of docker://postgres in Docker-in-Docker environments
CREATE DATABASE atlas_dev;
DO $$ BEGIN RAISE NOTICE 'Atlas dev database (atlas_dev) created'; END $$;

-- Create isolated database for integration tests
-- Tests use this instead of `app` so dev data is never affected
CREATE DATABASE app_test;
\connect app_test;
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "btree_gin";
\connect app;
DO $$ BEGIN RAISE NOTICE 'Integration test database (app_test) created'; END $$;
