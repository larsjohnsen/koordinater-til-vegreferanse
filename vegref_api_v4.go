// NVDB API Client Component (v4)
//
// This component communicates with the Norwegian Public Roads Administration (NVDB) API v4
// to convert UTM33 coordinates to road references (vegreferanse).
//
// Key features:
// - Implements the VegreferanseProvider interface
// - Makes requests to the NVDB API v4 /posisjon endpoint
// - Handles API rate limiting to comply with NVDB's usage policies
// - Integrates with the disk cache to reduce API calls
// - Processes and parses API responses
// - Returns vegreferanse matches with metadata for intelligent selection

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VegvesenetAPIV4 implements the VegreferanseProvider interface using the NVDB API v4
type VegvesenetAPIV4 struct {
	baseURL      string
	apiClient    *http.Client
	rateLimiter  *RateLimiter
	diskCache    *VegreferanseDiskCache
	searchRadius int // Search radius in meters
}

// V4PositionResponseItem represents a single item in the API response from the v4 API
type V4PositionResponseItem struct {
	Vegsystemreferanse struct {
		Vegsystem struct {
			Vegkategori string `json:"vegkategori"`
			Fase        string `json:"fase"`
			Nummer      int    `json:"nummer"`
		} `json:"vegsystem"`
		Strekning struct {
			Strekning       int     `json:"strekning"`
			Delstrekning    int     `json:"delstrekning"`
			Arm             bool    `json:"arm"`
			Adskilte_lop    string  `json:"adskilte_løp"`
			Trafikantgruppe string  `json:"trafikantgruppe"`
			Retning         string  `json:"retning"`
			Meter           float64 `json:"meter"`
		} `json:"strekning"`
		Kortform string `json:"kortform"`
	} `json:"vegsystemreferanse"`
	Veglenkesekvens struct {
		Veglenkesekvensid int     `json:"veglenkesekvensid"`
		RelativPosisjon   float64 `json:"relativPosisjon"`
		Kortform          string  `json:"kortform"`
	} `json:"veglenkesekvens"`
	Geometri struct {
		Wkt  string `json:"wkt"`
		Srid int    `json:"srid"`
	} `json:"geometri"`
	Kommune int     `json:"kommune"`
	Avstand float64 `json:"avstand"`
}

// V4PositionResponse is a slice of position response items
type V4PositionResponse []V4PositionResponseItem

// V4ErrorResponse represents the error response structure from the v4 API
type V4ErrorResponse struct {
	Messages []struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
		HelpURL string `json:"help_url"`
	} `json:"messages"`
	// For simple errors:
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// NewVegvesenetAPIV4 creates a new instance of the Vegvesenet API v4 client
func NewVegvesenetAPIV4(callsLimit int, timeFrame time.Duration, searchRadius int, diskCachePath string) *VegvesenetAPIV4 {
	var diskCache *VegreferanseDiskCache
	if diskCachePath != "" {
		var err error
		diskCache, err = NewVegreferanseDiskCache(diskCachePath)
		if err != nil {
			fmt.Printf("Warning: Failed to initialize disk cache: %v. Continuing without disk cache.\n", err)
		} else {
			fmt.Printf("Disk cache initialized at: %s\n", diskCachePath)
		}
	}

	return &VegvesenetAPIV4{
		baseURL:      "https://nvdbapiles.atlas.vegvesen.no",
		apiClient:    &http.Client{Timeout: 10 * time.Second},
		rateLimiter:  NewRateLimiter(callsLimit, timeFrame),
		diskCache:    diskCache,
		searchRadius: searchRadius,
	}
}

// GetVegreferanseFromCoordinates converts coordinates to a road reference using the NVDB API v4
func (api *VegvesenetAPIV4) GetVegreferanseFromCoordinates(x, y float64) (string, error) {
	// This implementation will select the first result
	matches, err := api.GetVegreferanseMatches(x, y)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", nil
	}

	// Use the first vegreferanse in the response
	return matches[0].Vegsystemreferanse.Kortform, nil
}

// VegreferanseMatch represents a single road match with associated metadata
type VegreferanseMatch struct {
	Vegsystemreferanse struct {
		Vegsystem struct {
			Vegkategori string `json:"vegkategori"`
			Fase        string `json:"fase"`
			Nummer      int    `json:"nummer"`
		} `json:"vegsystem"`
		Strekning struct {
			Strekning       int     `json:"strekning"`
			Delstrekning    int     `json:"delstrekning"`
			Arm             bool    `json:"arm"`
			Adskilte_lop    string  `json:"adskilte_løp"`
			Trafikantgruppe string  `json:"trafikantgruppe"`
			Retning         string  `json:"retning"`
			Meter           float64 `json:"meter"`
		} `json:"strekning"`
		Kortform string `json:"kortform"`
	} `json:"vegsystemreferanse"`
	Avstand float64 `json:"avstand"`
}

// GetVegreferanseMatches returns all matching vegreferanses for the given coordinates
func (api *VegvesenetAPIV4) GetVegreferanseMatches(x, y float64) ([]VegreferanseMatch, error) {
	// Check disk cache if available
	if api.diskCache != nil {
		if matches, found := api.diskCache.Get(x, y); found {
			return matches, nil
		}
	}

	// Apply rate limiting
	api.rateLimiter.Wait()

	// The v4 API uses a different endpoint for position lookups
	url := fmt.Sprintf("%s/posisjon", api.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters - using the UTM33 coordinates
	q := req.URL.Query()
	q.Add("nord", fmt.Sprintf("%.6f", y))                // Note: 'nord' is Y (northing)
	q.Add("ost", fmt.Sprintf("%.6f", x))                 // Note: 'ost' is X (easting)
	q.Add("srid", "5973")                                // UTM 33N EUREF89
	q.Add("radius", fmt.Sprintf("%d", api.searchRadius)) // Search radius in meters
	q.Add("maks_antall", "10")                           // Maximum number of results - now returning up to 10
	req.URL.RawQuery = q.Encode()

	// Add headers for v4 API
	req.Header.Add("Accept", "application/vnd.vegvesen.nvdb-v4+json")
	req.Header.Add("X-Client", "Koordinater til Vegreferanse")
	req.Header.Add("X-Client-Session", "402b9aee-16f9-e38d-2ce7-cd6bc20eb3e3")

	// Send request
	resp, err := api.apiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read full response body for error reporting
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			// Cache empty result for not found
			if api.diskCache != nil {
				_ = api.diskCache.Set(x, y, []VegreferanseMatch{})
			}
			return []VegreferanseMatch{}, nil
		}

		// Try to parse error response
		var errorResp V4ErrorResponse
		if jsonErr := json.Unmarshal(respBody, &errorResp); jsonErr == nil {
			// Check for either type of error format
			if len(errorResp.Messages) > 0 {
				errorMsg := ""
				for _, msg := range errorResp.Messages {
					errorMsg += fmt.Sprintf("[%d] %s ", msg.Code, msg.Message)
				}
				return nil, fmt.Errorf("API error: %s", errorMsg)
			} else if errorResp.Detail != "" {
				return nil, fmt.Errorf("API error: %s", errorResp.Detail)
			}
		}

		// If we couldn't parse the error, return raw status and body
		return nil, fmt.Errorf("API returned status code %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse successful response
	var result V4PositionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract matches
	if len(result) == 0 {
		// Cache empty result
		if api.diskCache != nil {
			_ = api.diskCache.Set(x, y, []VegreferanseMatch{})
		}
		return []VegreferanseMatch{}, nil
	}

	// Convert API response to our VegreferanseMatch struct
	matches := make([]VegreferanseMatch, len(result))
	for i, item := range result {
		matches[i] = VegreferanseMatch{
			Vegsystemreferanse: item.Vegsystemreferanse,
			Avstand:            item.Avstand,
		}
	}

	// Cache the matches
	if api.diskCache != nil {
		_ = api.diskCache.Set(x, y, matches)
	}

	return matches, nil
}

// LogAPIResponse is a helper function to log and inspect API responses during development
func (api *VegvesenetAPIV4) LogAPIResponse(x, y float64) (string, error) {
	url := fmt.Sprintf("%s/posisjon", api.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("nord", fmt.Sprintf("%.6f", y))                // 'nord' is Y (northing)
	q.Add("ost", fmt.Sprintf("%.6f", x))                 // 'ost' is X (easting)
	q.Add("srid", "5973")                                // UTM 33N EUREF89
	q.Add("radius", fmt.Sprintf("%d", api.searchRadius)) // Use the searchRadius parameter
	q.Add("maks_antall", "10")                           // Allow multiple results
	req.URL.RawQuery = q.Encode()

	// Add headers
	req.Header.Add("Accept", "application/vnd.vegvesen.nvdb-v4+json")
	req.Header.Add("X-Client", "Koordinater til Vegreferanse")
	req.Header.Add("X-Client-Session", "402b9aee-16f9-e38d-2ce7-cd6bc20eb3e3")

	// Send request
	resp, err := api.apiClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the full response body
	buf := new(strings.Builder)
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Return the full response as a string for inspection
	return fmt.Sprintf("Status: %s\nHeaders: %v\nBody: %s",
		resp.Status, resp.Header, buf.String()), nil
}
