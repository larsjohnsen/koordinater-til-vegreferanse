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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Global constants for API client
const (
	clientName = "Koordinater til Vegreferanse"
)

var clientSessionID string = uuid.NewString()

// VegvesenetAPIV4 implements the VegreferanseProvider interface using the NVDB API v4
type VegvesenetAPIV4 struct {
	baseURL     string
	apiClient   *http.Client
	rateLimiter *RateLimiter
	diskCache   *VegreferanseDiskCache
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
func NewVegvesenetAPIV4(callsLimit int, timeFrame time.Duration, diskCachePath string) *VegvesenetAPIV4 {
	var diskCache *VegreferanseDiskCache
	if diskCachePath != "" {
		var err error
		diskCache, err = NewVegreferanseDiskCache(diskCachePath)
		if err != nil {
			fmt.Printf("Warning: Failed to initialize disk cache: %v. Continuing without disk cache.\n", err)
		}
	}

	return &VegvesenetAPIV4{
		baseURL:     "https://nvdbapiles.atlas.vegvesen.no",
		apiClient:   &http.Client{Timeout: 10 * time.Second},
		rateLimiter: NewRateLimiter(callsLimit, timeFrame),
		diskCache:   diskCache,
	}
}

// createRequest creates a new HTTP request with common headers
func (api *VegvesenetAPIV4) createRequest(method, endpoint string) (*http.Request, error) {
	url := fmt.Sprintf("%s%s", api.baseURL, endpoint)

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add common headers for v4 API
	req.Header.Add("Accept", "application/vnd.vegvesen.nvdb-v4+json")
	req.Header.Add("X-Client", clientName)
	req.Header.Add("X-Client-Session", clientSessionID)

	return req, nil
}

// executeRequest executes an HTTP request and returns the response body
func (api *VegvesenetAPIV4) executeRequest(req *http.Request) ([]byte, int, error) {
	// Apply rate limiting
	api.rateLimiter.Wait()

	// Send request
	resp, err := api.apiClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read full response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// handleErrorResponse parses and returns a formatted error from an API error response
func (api *VegvesenetAPIV4) handleErrorResponse(statusCode int, respBody []byte) error {
	if statusCode == http.StatusNotFound {
		return nil // Not an error, just no results
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
			return fmt.Errorf("API error: %s", errorMsg)
		} else if errorResp.Detail != "" {
			return fmt.Errorf("API error: %s", errorResp.Detail)
		}
	}

	// If we couldn't parse the error, return raw status and body
	return fmt.Errorf("API returned status code %d: %s", statusCode, string(respBody))
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

	// Create request for position endpoint
	req, err := api.createRequest("GET", "/vegnett/api/v4/posisjon")
	if err != nil {
		return nil, err
	}

	// Add query parameters - using the UTM33 coordinates
	q := req.URL.Query()
	q.Add("nord", fmt.Sprintf("%.6f", y)) // Note: 'nord' is Y (northing)
	q.Add("ost", fmt.Sprintf("%.6f", x))  // Note: 'ost' is X (easting)
	q.Add("srid", "5973")                 // UTM 33N EUREF89
	q.Add("maks_antall", "10")            // Maximum number of results - now returning up to 10
	req.URL.RawQuery = q.Encode()

	// Execute request
	respBody, statusCode, err := api.executeRequest(req)
	if err != nil {
		return nil, err
	}

	// Handle non-200 responses
	if statusCode != http.StatusOK {
		if statusCode == http.StatusNotFound {
			// Cache empty result for not found
			if api.diskCache != nil {
				_ = api.diskCache.Set(x, y, []VegreferanseMatch{})
			}
			return []VegreferanseMatch{}, nil
		}

		return nil, api.handleErrorResponse(statusCode, respBody)
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

// GetCoordinatesFromVegreferanse returns UTM33 (EUREF89) coordinates for a given vegreferanse
func (api *VegvesenetAPIV4) GetCoordinatesFromVegreferanse(vegreferanse string) (Coordinate, error) {
	// Create the endpoint with the encoded vegreferanse
	encodedVegreferanse := url.QueryEscape(vegreferanse)
	endpoint := fmt.Sprintf("/vegnett/api/v4/veg/batch?vegsystemreferanser=%s", encodedVegreferanse)

	// Create request
	req, err := api.createRequest("GET", endpoint)
	if err != nil {
		return Coordinate{}, err
	}

	// Execute request
	respBody, statusCode, err := api.executeRequest(req)
	if err != nil {
		return Coordinate{}, err
	}

	// Handle non-200 responses
	if statusCode != http.StatusOK {
		if statusCode == http.StatusNotFound {
			return Coordinate{}, fmt.Errorf("vegreferanse not found: %s", vegreferanse)
		}

		return Coordinate{}, api.handleErrorResponse(statusCode, respBody)
	}

	// Parse the response to extract the WKT (Well-Known Text) geometry
	// Based on the actual response, the batch endpoint returns a map with vegreferanse as the key
	type LocationData struct {
		Geometri struct {
			Wkt  string `json:"wkt"`
			Srid int    `json:"srid"`
		} `json:"geometri"`
	}

	// Parse the response as a map with vegreferanse as keys
	var responseMap map[string]LocationData
	if err := json.Unmarshal(respBody, &responseMap); err != nil {
		return Coordinate{}, fmt.Errorf("failed to parse response: %w", err)
	}

	// Find the data for our vegreferanse
	locationData, found := responseMap[vegreferanse]
	if !found {
		return Coordinate{}, fmt.Errorf("no data found for vegreferanse: %s", vegreferanse)
	}

	// Parse WKT format to extract X and Y coordinates
	return parseWKTToCoordinate(locationData.Geometri.Wkt)
}

// parseWKTToCoordinate parses a WKT (Well-Known Text) string and extracts X and Y coordinates
func parseWKTToCoordinate(wkt string) (Coordinate, error) {
	if wkt == "" {
		return Coordinate{}, fmt.Errorf("empty WKT string")
	}

	// First extract the coordinate part from various WKT formats
	// POINT Z(x y z) or POINT Z (x y z) or POINT(x y) or POINT (x y)

	// Handle Z and ZM formats with and without space after the Z/ZM
	wkt = strings.ReplaceAll(wkt, "POINT Z(", "POINT Z (")
	wkt = strings.ReplaceAll(wkt, "POINT ZM(", "POINT ZM (")
	wkt = strings.ReplaceAll(wkt, "POINT M(", "POINT M (")
	wkt = strings.ReplaceAll(wkt, "POINT(", "POINT (")

	// Now trim the prefixes
	if strings.HasPrefix(wkt, "POINT Z (") {
		wkt = strings.TrimPrefix(wkt, "POINT Z (")
		wkt = strings.TrimSuffix(wkt, ")")
	} else if strings.HasPrefix(wkt, "POINT ZM (") {
		wkt = strings.TrimPrefix(wkt, "POINT ZM (")
		wkt = strings.TrimSuffix(wkt, ")")
	} else if strings.HasPrefix(wkt, "POINT M (") {
		wkt = strings.TrimPrefix(wkt, "POINT M (")
		wkt = strings.TrimSuffix(wkt, ")")
	} else if strings.HasPrefix(wkt, "POINT (") {
		wkt = strings.TrimPrefix(wkt, "POINT (")
		wkt = strings.TrimSuffix(wkt, ")")
	} else if strings.Contains(wkt, "EMPTY") {
		return Coordinate{}, fmt.Errorf("empty geometry in WKT: %s", wkt)
	} else {
		return Coordinate{}, fmt.Errorf("unrecognized WKT format: %s", wkt)
	}

	// Split the coordinates - only care about first two values (X, Y)
	parts := strings.Fields(wkt)
	if len(parts) < 2 {
		return Coordinate{}, fmt.Errorf("invalid WKT format, not enough coordinate values: %s", wkt)
	}

	// Parse X and Y
	x, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return Coordinate{}, fmt.Errorf("failed to parse X coordinate: %w", err)
	}

	y, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return Coordinate{}, fmt.Errorf("failed to parse Y coordinate: %w", err)
	}

	return Coordinate{X: x, Y: y}, nil
}
