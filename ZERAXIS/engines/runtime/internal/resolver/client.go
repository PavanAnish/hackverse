package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DiscoveryAsset represents a container asset from the Discovery API.
type DiscoveryAsset struct {
	AssetID     string `json:"asset_id"`
	Name        string `json:"name"`
	ContainerID string `json:"container_id"`
	Image       string `json:"image"`
	ImageID     string `json:"image_id"`
	Status      string `json:"status"`
}

// SupplyChainResult represents the supply chain data for an asset.
type SupplyChainResult struct {
	AssetID string           `json:"asset_id"`
	Image   SupplyChainImage `json:"image"`
}

// SupplyChainImage represents the image from the supply chain API.
type SupplyChainImage struct {
	ImageID    string `json:"image_id"`
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
}

// DiscoveryClient queries the Discovery and Supply Chain APIs.
type DiscoveryClient struct {
	discoveryURL   string
	supplyChainURL string
	httpClient     *http.Client
}

// NewDiscoveryClient creates a new client for asset resolution.
func NewDiscoveryClient(discoveryURL, supplyChainURL string) *DiscoveryClient {
	return &DiscoveryClient{
		discoveryURL:   discoveryURL,
		supplyChainURL: supplyChainURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FetchAssets retrieves all assets from the Discovery API.
func (c *DiscoveryClient) FetchAssets(ctx context.Context) ([]DiscoveryAsset, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.discoveryURL+"/api/v1/assets", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery API returned %d", resp.StatusCode)
	}

	var assets []DiscoveryAsset
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return nil, err
	}
	return assets, nil
}

// FetchImageDigest retrieves the immutable image digest for an asset from the Supply Chain API.
func (c *DiscoveryClient) FetchImageDigest(ctx context.Context, assetID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.supplyChainURL+"/api/v1/assets/"+assetID+"/supply-chain", nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("supply chain API returned %d for asset %s", resp.StatusCode, assetID)
	}

	var result SupplyChainResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Image.Digest == "" {
		return "", fmt.Errorf("no image digest found for asset %s", assetID)
	}

	return result.Image.Digest, nil
}
