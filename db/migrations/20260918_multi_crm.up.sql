-- Multi-Servis CRM module tables

CREATE TABLE IF NOT EXISTS multi_customers (
    id               TEXT PRIMARY KEY,
    first_name       TEXT NOT NULL,
    last_name        TEXT NOT NULL,
    phone            TEXT NOT NULL,
    normalized_phone TEXT NOT NULL,
    email            TEXT,
    normalized_email TEXT,
    customer_since   TIMESTAMPTZ NOT NULL DEFAULT now(),
    notes            TEXT NOT NULL DEFAULT '',
    archived_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_multi_customers_phone ON multi_customers(normalized_phone);
CREATE INDEX IF NOT EXISTS idx_multi_customers_email ON multi_customers(normalized_email);
CREATE INDEX IF NOT EXISTS idx_multi_customers_names ON multi_customers(last_name, first_name);
CREATE INDEX IF NOT EXISTS idx_multi_customers_archived ON multi_customers(archived_at);

CREATE TABLE IF NOT EXISTS multi_customer_licenses (
    id                 TEXT PRIMARY KEY,
    customer_id        TEXT NOT NULL REFERENCES multi_customers(id) ON DELETE RESTRICT,
    keygate_license_id TEXT NOT NULL REFERENCES licenses(id) ON DELETE RESTRICT,
    assigned_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at           TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_multi_customer_licenses_cust ON multi_customer_licenses(customer_id);
CREATE INDEX IF NOT EXISTS idx_multi_customer_licenses_lic ON multi_customer_licenses(keygate_license_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_multi_customer_licenses_active ON multi_customer_licenses(keygate_license_id) WHERE ended_at IS NULL;

CREATE TABLE IF NOT EXISTS multi_customer_events (
    id                 TEXT PRIMARY KEY,
    customer_id        TEXT NOT NULL REFERENCES multi_customers(id) ON DELETE RESTRICT,
    keygate_license_id TEXT REFERENCES licenses(id) ON DELETE SET NULL,
    event_type         TEXT NOT NULL,
    source             TEXT NOT NULL DEFAULT 'crm',
    external_event_id  TEXT,
    occurred_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    period_from        TIMESTAMPTZ,
    period_until       TIMESTAMPTZ,
    payment_method     TEXT,
    amount             NUMERIC(10, 2),
    currency           VARCHAR(10) NOT NULL DEFAULT 'PLN',
    notes              TEXT NOT NULL DEFAULT '',
    metadata           JSONB NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_multi_customer_events_cust ON multi_customer_events(customer_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_multi_customer_events_lic ON multi_customer_events(keygate_license_id);
CREATE INDEX IF NOT EXISTS idx_multi_customer_events_type ON multi_customer_events(event_type);
CREATE UNIQUE INDEX IF NOT EXISTS idx_multi_customer_events_external ON multi_customer_events(source, external_event_id) WHERE external_event_id IS NOT NULL;

