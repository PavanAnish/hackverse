import re

with open("engines/correlation/pkg/approvals/resolve_investigation_test.go", "r") as f:
    content = f.read()

content = content.replace(
    'db.QueryRow("SELECT actor_type FROM execution_state_transitions WHERE execution_id=$1 AND to_status=\'MANUALLY_REMEDIATED\'", execID).Scan(&actor)',
    'db.QueryRow("SELECT actor_type FROM execution_state_transitions WHERE execution_id=$1 AND to_status=\'ABANDONED\'", execID).Scan(&actor)'
)

with open("engines/correlation/pkg/approvals/resolve_investigation_test.go", "w") as f:
    f.write(content)
