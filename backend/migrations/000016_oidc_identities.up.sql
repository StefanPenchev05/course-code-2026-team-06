BEGIN;

-- One provider identity per account for the single-provider MVP. C collation
-- preserves exact, case-sensitive issuer/subject identity; never match by email.
CREATE TABLE oidc_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    issuer text COLLATE "C" NOT NULL,
    subject text COLLATE "C" NOT NULL,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    CONSTRAINT oidc_identities_identity_key UNIQUE (issuer, subject),
    CONSTRAINT oidc_identities_user_key UNIQUE (user_id),
    CONSTRAINT oidc_identities_issuer_check CHECK (
        octet_length(issuer) BETWEEN 1 AND 2048
        AND issuer ~ '^https?://[^/?#@[:space:]]+(/[^?#[:space:]]*)?$'
        AND issuer !~ '[[:cntrl:]]'
    ),
    CONSTRAINT oidc_identities_subject_check CHECK (
        octet_length(subject) BETWEEN 1 AND 255
        AND btrim(subject) <> '' AND subject !~ '[[:cntrl:]]'
    )
);

COMMIT;
