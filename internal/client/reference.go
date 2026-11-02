package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// Stable route templates for reference data and registry credentials.
const (
	routeComputeProfiles     = "/v1/reference/compute-profiles"
	routeRegistryCredentials = "/v1/organizations/{org}/registry-credentials"

	// registryCredentialPageSize is the largest page the route serves.
	registryCredentialPageSize = 100
	// maxRegistryCredentialPages bounds pagination so a server that never
	// stops returning cursors cannot spin the CLI forever.
	maxRegistryCredentialPages = 50
)

// ComputeProfileStatusActive marks a profile that can be selected.
const ComputeProfileStatusActive = "ACTIVE"

// ErrNoRecommendedProfile means the server marks no active compute profile as
// recommended, so the CLI has no server-sourced default to fall back on.
var ErrNoRecommendedProfile = errors.New("no recommended compute profile is available")

// ComputeProfile is one entry of the compute-profile reference catalog.
type ComputeProfile struct {
	Slug          string  `json:"slug"`
	DisplayName   string  `json:"displayName,omitempty"`
	Family        string  `json:"family,omitempty"`
	Tier          string  `json:"tier,omitempty"`
	Size          string  `json:"size,omitempty"`
	VCPUCount     int     `json:"vcpuCount,omitempty"`
	MemoryMB      int     `json:"memoryMb,omitempty"`
	MemoryGiB     float64 `json:"memoryGib,omitempty"`
	IsRecommended bool    `json:"isRecommended"`
	DisplayOrder  int     `json:"displayOrder,omitempty"`
	Status        string  `json:"status,omitempty"`
}

// computeProfileList is the envelope of GET /v1/reference/compute-profiles.
type computeProfileList struct {
	ComputeProfiles []ComputeProfile `json:"computeProfiles"`
}

// ListComputeProfiles returns the compute-profile catalog.
func (c *Client) ListComputeProfiles(ctx context.Context) ([]ComputeProfile, error) {
	list, _, err := do[computeProfileList](ctx, c, request{
		method: http.MethodGet,
		path:   []string{"v1", "reference", "compute-profiles"},
		op:     routeComputeProfiles,
	})
	if err != nil {
		return nil, err
	}

	return list.ComputeProfiles, nil
}

// RecommendedComputeProfile picks the server's recommended, active profile
// from a catalog. A profile with no status is treated as active, so an older
// catalog without the member still yields a default.
func RecommendedComputeProfile(profiles []ComputeProfile) (string, error) {
	for index := range profiles {
		profile := &profiles[index]
		if profile.IsRecommended && (profile.Status == "" || profile.Status == ComputeProfileStatusActive) {
			return profile.Slug, nil
		}
	}

	return "", ErrNoRecommendedProfile
}

// GetRecommendedComputeProfile returns the slug of the server's recommended
// compute profile: the default a deploy uses when musher.yaml names no size.
func (c *Client) GetRecommendedComputeProfile(ctx context.Context) (string, error) {
	profiles, err := c.ListComputeProfiles(ctx)
	if err != nil {
		return "", err
	}

	return RecommendedComputeProfile(profiles)
}

// RegistryCredential is a container-registry login an organization stored so
// the platform can pull private images. The secret itself is never returned.
type RegistryCredential struct {
	ID                        string  `json:"id"`
	OrganizationID            string  `json:"organizationId,omitempty"`
	DisplayName               string  `json:"displayName,omitempty"`
	RegistryHost              string  `json:"registryHost"`
	Username                  string  `json:"username,omitempty"`
	SecretLastFour            string  `json:"secretLastFour,omitempty"`
	VerificationStatus        string  `json:"verificationStatus,omitempty"`
	LastVerifiedAt            APITime `json:"lastVerifiedAt,omitzero"`
	LastVerificationErrorCode string  `json:"lastVerificationErrorCode,omitempty"`
	CreatedAt                 APITime `json:"createdAt,omitzero"`
	UpdatedAt                 APITime `json:"updatedAt,omitzero"`
}

// ListRegistryCredentials returns every registry credential an organization
// holds, following pagination.
func (c *Client) ListRegistryCredentials(ctx context.Context, orgID string) ([]RegistryCredential, error) {
	var (
		all    []RegistryCredential
		cursor string
	)

	for range maxRegistryCredentialPages {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(registryCredentialPageSize))

		if cursor != "" {
			query.Set("cursor", cursor)
		}

		page, _, err := do[Page[RegistryCredential]](ctx, c, request{
			method: http.MethodGet,
			path:   []string{"v1", "organizations", orgID, "registry-credentials"},
			query:  query,
			op:     routeRegistryCredentials,
		})
		if err != nil {
			return nil, err
		}

		all = append(all, page.Data...)

		if !page.Meta.HasMore || page.Meta.NextCursor == "" {
			break
		}

		cursor = page.Meta.NextCursor
	}

	return all, nil
}
