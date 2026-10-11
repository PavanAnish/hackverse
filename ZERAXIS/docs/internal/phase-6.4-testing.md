# Testing

Phase 6.4 includes strict adversarial unit tests and regression testing.

## Unit Tests
Located in `engines/correlation/internal/recommender/recommender_test.go`.
* **CRITICAL + STRONG evidence**: Yields high-priority quarantine actions.
* **CRITICAL + UNKNOWN evidence**: Suppresses disruptive recommendations.
* **Missing asset_id**: Gracefully skips asset-targeted recommendations.
* **Malicious metadata**: Proves that injected SQL/shell strings are treated purely as data.

## Integration Tests
Verified via `make test-health` and `make test-runtime` which run successfully, validating end-to-end event flow without breaking Phase 5 or Phase 6.2/6.3 boundaries.
