// Package docker provides a read-only wrapper around the Docker Engine API.
//
// SECURITY: This package deliberately exposes NO write operations
// (ContainerStop, ContainerRemove, ContainerCreate, etc.). The discovery
// engine must only observe infrastructure — never modify it. Write
// operations belong to the future Response Engine (Phase 8).
//
// The Docker socket (/var/run/docker.sock) is a privileged security
// boundary. Mounting it into a container grants full Docker API access
// regardless of the :ro mount flag. The :ro flag only prevents the
// container from deleting or replacing the socket file itself — it does
// NOT restrict Docker API operations. Read-only behavior is enforced
// entirely by this code-level wrapper refusing to call write endpoints.
package docker

import (
	"context"
	"fmt"
	"strings"

	containertypes "github.com/docker/docker/api/types/container"
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Client wraps the Docker Engine API client with read-only operations.
type Client struct {
	cli *client.Client
}

// ContainerInfo holds inspected container data.
type ContainerInfo struct {
	ID         string
	Name       string // cleaned, no leading /
	Image      string
	ImageID    string
	Status     string // "running", "exited", "paused", "created"
	State      string
	CreatedAt  string
	StartedAt  string
	Labels     map[string]string
	Ports      []PortBinding
	Networks   []NetworkInfo
}

// PortBinding represents a port exposed or published by a container.
type PortBinding struct {
	ContainerPort int
	HostPort      int
	Protocol      string
	HostIP        string
}

// NetworkInfo represents a container's membership in a network.
type NetworkInfo struct {
	Name      string
	IPAddress string
	Gateway   string
}

// NewClient creates a new read-only Docker client.
// It uses the default Docker socket or DOCKER_HOST environment variable.
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Ping verifies the Docker daemon is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// ListContainers returns all containers (running and stopped).
func (c *Client) ListContainers(ctx context.Context) ([]containertypes.Summary, error) {
	return c.cli.ContainerList(ctx, containertypes.ListOptions{All: true})
}

// InspectContainer returns detailed information about a specific container.
func (c *Client) InspectContainer(ctx context.Context, containerID string) (*ContainerInfo, error) {
	inspect, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	info := &ContainerInfo{
		ID:        inspect.ID[:12],
		Name:      strings.TrimPrefix(inspect.Name, "/"),
		Image:     inspect.Config.Image,
		ImageID:   inspect.Image,
		Status:    strings.ToLower(inspect.State.Status),
		State:     inspect.State.Status,
		CreatedAt: inspect.Created,
		Labels:    inspect.Config.Labels,
	}

	if inspect.State != nil {
		info.StartedAt = inspect.State.StartedAt
	}

	// Extract port bindings
	if inspect.HostConfig != nil {
		for port, bindings := range inspect.HostConfig.PortBindings {
			containerPort := port.Int()
			protocol := port.Proto()

			if len(bindings) == 0 {
				info.Ports = append(info.Ports, PortBinding{
					ContainerPort: containerPort,
					Protocol:      protocol,
				})
				continue
			}

			for _, b := range bindings {
				hostPort := 0
				if b.HostPort != "" {
					fmt.Sscanf(b.HostPort, "%d", &hostPort)
				}
				info.Ports = append(info.Ports, PortBinding{
					ContainerPort: containerPort,
					HostPort:      hostPort,
					Protocol:      protocol,
					HostIP:        b.HostIP,
				})
			}
		}
	}

	// Also get ports from NetworkSettings for exposed-only ports
	if inspect.NetworkSettings != nil {
		for port := range inspect.NetworkSettings.Ports {
			containerPort := port.Int()
			protocol := port.Proto()
			// Check if we already have this port from HostConfig
			found := false
			for _, p := range info.Ports {
				if p.ContainerPort == containerPort && p.Protocol == protocol {
					found = true
					break
				}
			}
			if !found {
				info.Ports = append(info.Ports, PortBinding{
					ContainerPort: containerPort,
					Protocol:      protocol,
				})
			}
		}
	}

	// Extract networks
	if inspect.NetworkSettings != nil && inspect.NetworkSettings.Networks != nil {
		for name, net := range inspect.NetworkSettings.Networks {
			info.Networks = append(info.Networks, NetworkInfo{
				Name:      name,
				IPAddress: net.IPAddress,
				Gateway:   net.Gateway,
			})
		}
	}

	return info, nil
}

// ListNetworks returns all Docker networks.
func (c *Client) ListNetworks(ctx context.Context) ([]networktypes.Summary, error) {
	return c.cli.NetworkList(ctx, networktypes.ListOptions{})
}

// Close closes the Docker client connection.
func (c *Client) Close() error {
	return c.cli.Close()
}
