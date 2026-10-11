-- Migration 002: Cleanup Mutable Identities
-- Delete images (and their cascades: sboms, packages, vulns) that were created
-- using the SHA256 of the string tag reference rather than the true OCI digest.
-- These cannot be safely reconciled because the registry does not keep a history
-- of what digest a tag pointed to in the past.

DELETE FROM supply_chain_images
WHERE image_id NOT LIKE 'sha256:%';
