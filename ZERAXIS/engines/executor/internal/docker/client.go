package docker

import (
	"context"

	"github.com/docker/docker/client"
)

type Client interface {
	ContainerStop(ctx context.Context, containerID string) error
	NetworkDisconnect(ctx context.Context, containerID, networkID string) error
	InspectContainer(ctx context.Context, containerID string) (*ContainerState, error)
}

type ContainerState struct {
	ID          string
	ImageID     string
	RepoDigests []string
	State       string
	NetworkIDs  []string
}

type RealClient struct {
	cli *client.Client
}

func NewRealClient() (*RealClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &RealClient{cli: cli}, nil
}

func (r *RealClient) ContainerStop(ctx context.Context, containerID string) error {
	// For older sdk compatibility, pass nil for timeout
	return r.cli.ContainerStop(ctx, containerID, nil)
}

func (r *RealClient) NetworkDisconnect(ctx context.Context, containerID, networkID string) error {
	return r.cli.NetworkDisconnect(ctx, networkID, containerID, false)
}

func (r *RealClient) InspectContainer(ctx context.Context, containerID string) (*ContainerState, error) {
	inspect, err := r.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	state := &ContainerState{
		ID:          inspect.ID,
		ImageID:     inspect.Image,
		RepoDigests: nil, // Wait, inspect.Image is a string. Actually inspect doesn't give repodigests directly in ContainerInspect.
		State:       inspect.State.Status,
	}

	// We fetch the actual image details to get RepoDigests
	imgInspect, _, err := r.cli.ImageInspectWithRaw(ctx, inspect.Image)
	if err == nil {
		state.RepoDigests = imgInspect.RepoDigests
	}

	for netName := range inspect.NetworkSettings.Networks {
		state.NetworkIDs = append(state.NetworkIDs, netName)
	}

	return state, nil
}

