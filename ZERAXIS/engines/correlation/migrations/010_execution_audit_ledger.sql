CREATE TABLE IF NOT EXISTS execution_state_transitions (
    transition_id UUID PRIMARY KEY,
    execution_id UUID NOT NULL REFERENCES execution_authorizations(execution_id) ON DELETE CASCADE,
    from_status TEXT,
    to_status TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason TEXT,
    correlation_id TEXT,
    trace_id TEXT
);

CREATE OR REPLACE FUNCTION prevent_ledger_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Updates and Deletes are explicitly prohibited on execution_state_transitions';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS enforce_append_only_ledger ON execution_state_transitions;
CREATE TRIGGER enforce_append_only_ledger
BEFORE UPDATE OR DELETE ON execution_state_transitions
FOR EACH ROW EXECUTE FUNCTION prevent_ledger_modification();
