-- Phase 7.3: Post-Execution Verification
-- Extends execution_results with verification status columns.
-- NULL is preserved for legacy Phase 7.2 rows where verification was not performed.
-- Phase 7.3 execution paths MUST NOT produce EXECUTED + NULL.

ALTER TABLE execution_results
DROP CONSTRAINT IF EXISTS chk_verification_status;
ALTER TABLE execution_results
ADD COLUMN IF NOT EXISTS verification_status TEXT,
ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;

-- Enforce that verification_status is NULL or a valid terminal verdict.
ALTER TABLE execution_results
DROP CONSTRAINT IF EXISTS chk_verification_status;
ALTER TABLE execution_results
ADD CONSTRAINT chk_verification_status
CHECK (
    verification_status IS NULL
    OR verification_status IN ('VERIFIED', 'FAILED')
);
