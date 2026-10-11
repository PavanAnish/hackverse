with open("engines/correlation/pkg/approvals/service.go", "r") as f:
    content = f.read()

target2 = """\t\t_, err = tx.ExecContext(ctx, `
\t\t\tINSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, status, priority, reason, evidence, model_version, target_asset_id, target_container_id, target_image_digest)
\t\t\tVALUES ($1, $2, $3, $4, 'PROPOSED', $5, $6, $7, $8, $9, $10, $11)
\t\t`, newRecID, incID, uuid.New().String(), action, priority, rReason, evidence, modelVersion, tAsset, tContainer, tDigest)"""

replacement2 = """\t\tbucketStr := time.Now().UTC().Format("2006-01-02-15")
\t\trawFP := fmt.Sprintf("%s:%s:%s", tAsset.String, tDigest.String, bucketStr)
\t\th := sha256.Sum256([]byte(rawFP))
\t\tfingerprint := fmt.Sprintf("%x", h)
\t\t_, err = tx.ExecContext(ctx, `
\t\t\tINSERT INTO incident_recommendations (recommendation_id, incident_id, fingerprint, action, status, priority, reason, evidence, model_version, target_asset_id, target_container_id, target_image_digest)
\t\t\tVALUES ($1, $2, $3, $4, 'PROPOSED', $5, $6, $7, $8, $9, $10, $11)
\t\t`, newRecID, incID, fingerprint, action, priority, rReason, evidence, modelVersion, tAsset, tContainer, tDigest)"""

if target2 in content:
    content = content.replace(target2, replacement2)
else:
    print("TARGET 2 NOT FOUND")

target3 = """\tif execID != "" {
\t\ttoStatus := "MANUALLY_REMEDIATED"
\t\tif outcome == "OUTCOME_SAFE_TO_RETRY" {
\t\t\ttoStatus = "SUPERSEDED"
\t\t}
\t\ttransID := uuid.New().String()
\t\ttx.ExecContext(ctx, `
\t\t\tINSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
\t\t\tVALUES ($1, $2, 'MANUAL_INVESTIGATION_REQUIRED', $3, 'human', $4, NOW(), $5)
\t\t`, transID, execID, toStatus, operatorID, outcome+": "+reason)
\t}"""

replacement3 = """\tif execID != "" {
\t\tvar currentExecStatus string
\t\terr := tx.QueryRowContext(ctx, `SELECT status FROM execution_authorizations WHERE execution_id = $1`, execID).Scan(&currentExecStatus)
\t\tif err == nil && (currentExecStatus == "ABANDONED" || currentExecStatus == "FAILED" || currentExecStatus == "EXECUTED") {
\t\t\ttransID := uuid.New().String()
\t\t\ttx.ExecContext(ctx, `
\t\t\t\tINSERT INTO execution_state_transitions (transition_id, execution_id, from_status, to_status, actor_type, actor_id, timestamp, reason)
\t\t\t\tVALUES ($1, $2, $3, $4, 'human', $5, NOW(), $6)
\t\t\t`, transID, execID, currentExecStatus, currentExecStatus, operatorID, outcome+": "+reason)
\t\t}
\t}"""

if target3 in content:
    content = content.replace(target3, replacement3)
else:
    print("TARGET 3 NOT FOUND")

if '"crypto/sha256"' not in content:
    content = content.replace('"database/sql"', '"database/sql"\n\t"crypto/sha256"')

with open("engines/correlation/pkg/approvals/service.go", "w") as f:
    f.write(content)
