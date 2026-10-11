package evaluator

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/open-policy-agent/opa/rego"
)

func LoadActivePolicy(db *sql.DB) (policyText string, version string, err error) {
	err = db.QueryRow("SELECT rego_text, policy_id::text FROM policy_definitions WHERE policy_status = 'ACTIVE' LIMIT 1").
		Scan(&policyText, &version)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", fmt.Errorf("no active policy found")
		}
		return "", "", fmt.Errorf("failed to load active policy: %w", err)
	}
	return policyText, version, nil
}

func CompilePolicy(regoText string) (interface{}, error) {
	query, err := rego.New(
		rego.Query("data.shield.admission"),
		rego.Module("policy.rego", regoText),
	).PrepareForEval(context.Background())
	
	if err != nil {
		return nil, fmt.Errorf("failed to compile policy: %w", err)
	}
	return query, nil
}
