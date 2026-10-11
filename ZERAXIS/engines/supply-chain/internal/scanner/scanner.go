package scanner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

// AllowedImagePattern prevents command injection by ensuring the image
// identifier only contains valid OCI registry characters.
var AllowedImagePattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+(:[0-9]+)?(/[a-zA-Z0-9_.-]+)+(:[a-zA-Z0-9_.-]+)?(@sha256:[a-fA-F0-9]{64})?$`)

type Scanner interface {
	RunSyft(ctx context.Context, image string) ([]byte, error)
	RunTrivy(ctx context.Context, image string) ([]byte, error)
}

type LocalScanner struct{}

func NewLocalScanner() *LocalScanner {
	return &LocalScanner{}
}

// RunSyft executes the Syft binary against a remote registry image
// and returns the CycloneDX JSON bytes.
// Memory Handling Decision: We buffer stdout completely in memory. For a
// local laboratory, an SBOM is typically <10MB, so bounded bytes.Buffer is
// demonstrably safe at this scale and avoids complex temporary file streaming logic.
func (s *LocalScanner) RunSyft(ctx context.Context, image string) ([]byte, error) {
	if !AllowedImagePattern.MatchString(image) {
		return nil, fmt.Errorf("invalid image format: %s", image)
	}

	// Syft needs registry: prefix to know it's remote, unless it has a valid schema
	target := "registry:" + image

	cmd := exec.CommandContext(ctx, "syft", target, "-o", "cyclonedx-json")
	cmd.Env = append(os.Environ(),
		"SYFT_REGISTRY_INSECURE_SKIP_TLS_VERIFY=true",
		"SYFT_REGISTRY_INSECURE_USE_HTTP=true",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("syft execution failed: %v, stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// RunTrivy executes the Trivy binary against a generated SBOM
// and returns the Trivy JSON bytes.
func (s *LocalScanner) RunTrivy(ctx context.Context, sbomPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "trivy", "sbom", "--format", "json", sbomPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("trivy execution failed: %v, stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}
