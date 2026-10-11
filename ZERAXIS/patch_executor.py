with open("engines/executor/internal/engine/executor.go", "r") as f:
    content = f.read()

target = """\tcase "STOP_CONTAINER":
\t\tif !terminalContainerStates[state.State] {
\t\t\treturn verificationResult{
\t\t\t\tstatus: "FAILED",
\t\t\t\treason: fmt.Sprintf("container state %q is not terminal (want exited or dead)", state.State),
\t\t\t}
\t\t}
\t// QUARANTINE_ASSET verification is not in scope for Phase 7.3.
\t// We yield VERIFIED conservatively; a future phase can extend this.
\t}"""

replacement = """\tcase "STOP_CONTAINER":
\t\tif !terminalContainerStates[state.State] {
\t\t\treturn verificationResult{
\t\t\t\tstatus: "FAILED",
\t\t\t\treason: fmt.Sprintf("container state %q is not terminal (want exited or dead)", state.State),
\t\t\t}
\t\t}
\tcase "QUARANTINE_ASSET":
\t\tauthNet := os.Getenv("AUTHORIZED_QUARANTINE_NETWORK")
\t\tif authNet == "" {
\t\t\treturn verificationResult{
\t\t\t\tstatus: "FAILED",
\t\t\t\treason: "AUTHORIZED_QUARANTINE_NETWORK not configured",
\t\t\t}
\t\t}
\t\tfor _, net := range state.NetworkIDs {
\t\t\tif net == authNet {
\t\t\t\treturn verificationResult{
\t\t\t\t\tstatus: "FAILED",
\t\t\t\t\treason: fmt.Sprintf("container still attached to quarantine network %q", authNet),
\t\t\t\t}
\t\t\t}
\t\t}
\t}"""

if target in content:
    content = content.replace(target, replacement)
else:
    print("TARGET NOT FOUND IN EXECUTOR")

with open("engines/executor/internal/engine/executor.go", "w") as f:
    f.write(content)
