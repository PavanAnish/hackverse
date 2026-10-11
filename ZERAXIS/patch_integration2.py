import os
import re

os.chdir("engines/executor/internal/engine")

file = "executor_integration_test.go"
with open(file, "r") as f:
    content = f.read()

# Try removing import "database/sql"
content = re.sub(r'"database/sql"\n', '', content)

with open(file, "w") as f:
    f.write(content)

