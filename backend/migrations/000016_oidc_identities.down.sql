BEGIN;

-- Removing a populated login-identity map can orphan the only login path.
-- Require an explicit future migration/operational decision rather than silently
-- forgetting bindings. An empty disposable database can roll back normally.
-- Serialize the emptiness check with both mapping writers and the final DROP.
LOCK TABLE oidc_identities IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM oidc_identities) THEN
        RAISE EXCEPTION 'cannot roll back OIDC identity bindings while mappings exist';
    END IF;
END;
$$;
DROP TABLE oidc_identities;

COMMIT;
