-- One user owns one business in this version. The foreign key lets a later version add more users to a business.
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    -- always lower case, the service lowers it before it reads or writes
    email text NOT NULL,
    -- null for an account that only signs in with Google
    password_hash text,
    google_subject text,
    email_verified_at timestamptz,
    consent_at timestamptz NOT NULL DEFAULT now(),
    consent_version text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_lower CHECK (email = lower(email) AND char_length(email) BETWEEN 3 AND 254),
    CONSTRAINT users_has_a_way_to_sign_in CHECK (password_hash IS NOT NULL OR google_subject IS NOT NULL)
);

CREATE UNIQUE INDEX users_email_key ON users (email);
CREATE UNIQUE INDEX users_google_subject_key ON users (google_subject) WHERE google_subject IS NOT NULL;
CREATE INDEX users_business_id_idx ON users (business_id);

-- The primary key is the SHA-256 of the token, so a copy of the table cannot sign anybody in.
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sessions_hash_length CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_last_used_at_idx ON sessions (last_used_at);

-- A link in an email. The primary key is the SHA-256 of the token. A token works once.
CREATE TABLE email_tokens (
    token_hash bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    CONSTRAINT email_tokens_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT email_tokens_purpose CHECK (purpose IN ('verify', 'reset'))
);

CREATE INDEX email_tokens_user_id_idx ON email_tokens (user_id, purpose);
CREATE INDEX email_tokens_expires_at_idx ON email_tokens (expires_at);

-- One row for each request that a limit counts. The limits live here and not in memory, so both copies of the service share them.
CREATE TABLE rate_limit_events (
    bucket text NOT NULL,
    key text NOT NULL,
    at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX rate_limit_events_lookup_idx ON rate_limit_events (bucket, key, at);
