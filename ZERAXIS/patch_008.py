import re

with open("engines/correlation/migrations/008_execution_verification.sql", "r") as f:
    content = f.read()

content = content.replace("ALTER TABLE execution_results\nADD CONSTRAINT chk_verification_status", "ALTER TABLE execution_results DROP CONSTRAINT IF EXISTS chk_verification_status;\nALTER TABLE execution_results\nADD CONSTRAINT chk_verification_status")

with open("engines/correlation/migrations/008_execution_verification.sql", "w") as f:
    f.write(content)
