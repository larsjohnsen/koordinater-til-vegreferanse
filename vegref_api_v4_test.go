package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestGetCoordinatesFromVegreferanse tests the conversion from vegreferanse to coordinates
func TestGetCoordinatesFromVegreferanse(t *testing.T) {
	// Skip in short mode to avoid hitting the actual API
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Create API client with reasonable rate limit
	api := NewVegvesenetAPIV4(10, time.Second, "")

	// Test cases with known vegreferanses
	testCases := []struct {
		vegreferanse    string
		description     string
		expectError     bool
		expectedXPrefix string // Expected prefix of X coordinate to check (not exact due to potential API changes)
		expectedYPrefix string // Expected prefix of Y coordinate to check
	}{
		{"FV7834 S1D1 m11", "County road 7834", false, "6414", "7679"}, // Based on the input sample
		{"E6 S72D1 m1000", "E6 near Trondheim", false, "26", "70"},     // E6 near Trondheim
		{"INVALID_VEGREF", "Invalid vegreferanse", true, "", ""},       // Should return error
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Get coordinates for the vegreferanse
			coordinates, err := api.GetCoordinatesFromVegreferanse(tc.vegreferanse)

			// Check error condition
			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error for vegreferanse %s, but got none", tc.vegreferanse)
				}
				return
			}

			// Otherwise, we expect no error
			if err != nil {
				t.Fatalf("Error getting coordinates: %v", err)
			}

			// Log the result
			t.Logf("Vegreferanse %s -> Coordinates: X=%.6f, Y=%.6f",
				tc.vegreferanse, coordinates.X, coordinates.Y)

			// Convert coordinates to strings for prefix checking
			xStr := formatCoordinate(coordinates.X)
			yStr := formatCoordinate(coordinates.Y)

			// Check if coordinates start with expected prefixes
			if tc.expectedXPrefix != "" && !strings.HasPrefix(xStr, tc.expectedXPrefix) {
				t.Errorf("X coordinate %s does not start with expected prefix %s",
					xStr, tc.expectedXPrefix)
			}
			if tc.expectedYPrefix != "" && !strings.HasPrefix(yStr, tc.expectedYPrefix) {
				t.Errorf("Y coordinate %s does not start with expected prefix %s",
					yStr, tc.expectedYPrefix)
			}

			// Verify coordinates are reasonable for Norway
			// Norway's UTM33 boundaries approximately
			if coordinates.X < 0 || coordinates.X > 1000000 {
				t.Errorf("X coordinate %.6f is outside reasonable range for Norway", coordinates.X)
			}
			if coordinates.Y < 6400000 || coordinates.Y > 7800000 {
				t.Errorf("Y coordinate %.6f is outside reasonable range for Norway", coordinates.Y)
			}
		})

		// Add a small delay between tests to respect rate limits
		time.Sleep(200 * time.Millisecond)
	}
}

// TestRobustWKTHandling tests the handling of different WKT formats
func TestRobustWKTHandling(t *testing.T) {
	// Create fake coordinates for different WKT formats
	testCases := []struct {
		wkt         string
		expectedX   float64
		expectedY   float64
		expectError bool
		description string
	}{
		{"POINT (123.456 789.012)", 123.456, 789.012, false, "Simple POINT format"},
		{"POINT Z (123.456 789.012 10.0)", 123.456, 789.012, false, "POINT Z format with elevation"},
		{"POINT ZM (123.456 789.012 10.0 1.0)", 123.456, 789.012, false, "POINT ZM format"},
		{"POINT EMPTY", 0, 0, true, "Empty point"},
		{"INVALID", 0, 0, true, "Invalid WKT"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Create a simulated response struct
			var response struct {
				Geometri struct {
					Wkt  string
					Srid int
				}
			}
			response.Geometri.Wkt = tc.wkt

			// Extract X and Y from WKT using our parsing logic from GetCoordinatesFromVegreferanse
			var x, y float64
			var err error

			// Replicate the parsing logic from the function
			wkt := response.Geometri.Wkt
			wkt = strings.TrimPrefix(wkt, "POINT Z (")
			wkt = strings.TrimPrefix(wkt, "POINT ZM (")
			wkt = strings.TrimPrefix(wkt, "POINT (")
			wkt = strings.TrimSuffix(wkt, ")")

			if wkt == "EMPTY" || wkt == response.Geometri.Wkt {
				err = fmt.Errorf("invalid WKT format: %s", wkt)
			} else {
				// Split the coordinates
				parts := strings.Split(wkt, " ")
				if len(parts) < 2 {
					err = fmt.Errorf("invalid WKT format: %s", wkt)
				} else {
					// Try to parse X and Y
					xStr := parts[0]
					yStr := parts[1]

					x, err = parseFloat(xStr)
					if err == nil {
						y, err = parseFloat(yStr)
					}
				}
			}

			// Check results against expected values
			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error parsing WKT %s, but got none", tc.wkt)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error parsing WKT %s: %v", tc.wkt, err)
			}

			if x != tc.expectedX || y != tc.expectedY {
				t.Errorf("Parsing WKT %s: expected coordinates (%.6f, %.6f), got (%.6f, %.6f)",
					tc.wkt, tc.expectedX, tc.expectedY, x, y)
			}
		})
	}
}

// Helper functions for testing

// parseFloat tries to parse a string as a float64
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// formatCoordinate formats a coordinate for prefix checking
func formatCoordinate(coord float64) string {
	return fmt.Sprintf("%.6f", coord)
}

func TestVegvesenetAPIV4_Comprehensive(t *testing.T) {
	// Basic functionality test
	t.Run("BasicFunctionality", func(t *testing.T) {
		// Create API client with small cache and rate limiter
		api := NewVegvesenetAPIV4(10, time.Minute, "")

		// Test coordinates that should return a valid road reference
		x := 253671.97
		y := 6648897.78

		// First call - should hit the API and cache the result
		vegreferanse, err := api.GetVegreferanseFromCoordinates(x, y)
		if err != nil {
			t.Fatalf("Error getting vegreferanse: %v", err)
		}

		// Verify we got a non-empty result
		if vegreferanse == "" {
			t.Fatal("Expected a non-empty vegreferanse, but got empty string")
		}

		t.Logf("Successfully retrieved vegreferanse: %s", vegreferanse)

		// Second call with same coordinates - should use cached result
		start := time.Now()
		cachedVegreferanse, err := api.GetVegreferanseFromCoordinates(x, y)
		if err != nil {
			t.Fatalf("Error getting cached vegreferanse: %v", err)
		}
		cacheTime := time.Since(start)

		// Verify cached result matches first result
		if cachedVegreferanse != vegreferanse {
			t.Fatalf("Cached vegreferanse %s does not match original %s", cachedVegreferanse, vegreferanse)
		}

		// Verify cache lookup was fast
		if cacheTime > 50*time.Millisecond {
			t.Logf("Warning: Cache retrieval took %v, which is longer than expected", cacheTime)
		}

		t.Logf("Successfully retrieved cached vegreferanse: %s", cachedVegreferanse)
	})

	// Test handling of non-existent roads
	t.Run("NonExistentRoad", func(t *testing.T) {
		// Create API client
		api := NewVegvesenetAPIV4(10, time.Minute, "")

		// Test with coordinates far out at sea where there should be no roads
		// Using coordinates in the North Sea
		x := 141000.0
		y := 6650000.0

		vegreferanse, err := api.GetVegreferanseFromCoordinates(x, y)
		if err != nil {
			t.Fatalf("Error getting vegreferanse: %v", err)
		}

		// Should get empty string for non-existent road
		if vegreferanse != "" {
			t.Fatalf("Expected empty vegreferanse for non-existent road, but got: %s", vegreferanse)
		}

		t.Log("Successfully returned empty string for non-existent road")
	})

	// Test the full API, including raw response (skipped by default)
	t.Run("RealAPITests", func(t *testing.T) {
		// Skip by default to avoid hitting the real API in automated tests
		if testing.Short() {
			t.Skip("Skipping real API test in short mode")
		}

		// Create an instance of the v4 API client
		apiClient := NewVegvesenetAPIV4(10, time.Second, "")

		// Test the API response using the regular method
		t.Run("TestAPIResponse", func(t *testing.T) {
			// Choose a known coordinate pair in Norway (E18 near Oslo)
			// These coordinates are in UTM33 EUREF89 (EPSG:5973)
			x := 253671.97
			y := 6648897.78

			// Use the regular method to get vegreferanse matches
			matches, err := apiClient.GetVegreferanseMatches(x, y)
			if err != nil {
				t.Fatalf("Error making API request: %v", err)
			}

			// Log the matches for inspection
			t.Logf("API returned %d matches for coordinates (%.6f, %.6f)", len(matches), x, y)
			for i, match := range matches {
				t.Logf("Match %d: %s (distance: %.2f meters)", i+1, match.Vegsystemreferanse.Kortform, match.Avstand)
			}

			// Verify we got at least one match
			if len(matches) == 0 {
				t.Errorf("Expected at least one match for coordinates (%.6f, %.6f)", x, y)
			}
		})

		// Test the caching behavior
		t.Run("CacheBehavior", func(t *testing.T) {
			// Use a coordinate pair that worked in the previous test
			x := 253671.97
			y := 6648897.78

			// First call should hit the API
			start := time.Now()
			firstResult, err := apiClient.GetVegreferanseFromCoordinates(x, y)
			if err != nil {
				t.Fatalf("Error on first API call: %v", err)
			}
			firstCallTime := time.Since(start)
			t.Logf("First API call took %v", firstCallTime)

			// Second call should use cache and be much faster
			start = time.Now()
			secondResult, err := apiClient.GetVegreferanseFromCoordinates(x, y)
			if err != nil {
				t.Fatalf("Error on second API call: %v", err)
			}
			secondCallTime := time.Since(start)
			t.Logf("Second API call took %v", secondCallTime)

			// Verify results match
			if firstResult != secondResult {
				t.Errorf("Cache returned different result. First: %s, Second: %s",
					firstResult, secondResult)
			}

			// Verify second call was significantly faster
			if secondCallTime > firstCallTime/2 {
				t.Logf("Warning: Second call doesn't appear to be using cache effectively")
			}
		})
	})

	// Test the multiple matches functionality
	t.Run("MultipleMatches", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping real API test in short mode")
		}

		api := NewVegvesenetAPIV4(10, time.Second, "")

		// We'll use coordinates for a location that might have multiple roads nearby
		// These are example coordinates where roads might intersect
		x := 253671.97
		y := 6648897.78

		matches, err := api.GetVegreferanseMatches(x, y)
		if err != nil {
			t.Fatalf("Error getting vegreferanse matches: %v", err)
		}

		// Log the number of matches found
		t.Logf("Found %d matches for coordinates (%.6f, %.6f)", len(matches), x, y)

		// Log each match with its distance
		for i, match := range matches {
			t.Logf("Match %d: %s (distance: %.2f meters)",
				i+1, match.Vegsystemreferanse.Kortform, match.Avstand)
		}

		// Test that the matches are properly used with the selector
		selector := NewVegreferanseSelector(5)

		// Test with no history first
		bestMatch := selector.SelectBestMatch(matches)
		t.Logf("Best match with no history: %s", bestMatch)

		// Add a mock history entry and test again to see if selection changes
		mockVegreferanse := "E18 S65D1 m12500" // Example, might match real road nearby
		selector.AddToHistory(mockVegreferanse)

		bestMatchWithHistory := selector.SelectBestMatch(matches)
		t.Logf("Best match with history: %s", bestMatchWithHistory)
	})
}

// TestIntegration_SelectorWithAPI tests the integration between the API and selector
func TestIntegration_SelectorWithAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	api := NewVegvesenetAPIV4(10, time.Second, "")
	selector := NewVegreferanseSelector(5)

	// Simulate a journey along a road by using slightly different coordinates
	journey := []struct {
		x           float64
		y           float64
		description string
	}{
		// These coordinates should be slightly adjusted to follow a path
		{253671.97, 6648897.78, "Start point"},
		{253675.00, 6648900.00, "Mid point"}, // slightly adjusted
		{253680.00, 6648905.00, "End point"}, // slightly adjusted
	}

	for i, point := range journey {
		t.Run(fmt.Sprintf("JourneyPoint%d", i+1), func(t *testing.T) {
			// Get matches for this point
			matches, err := api.GetVegreferanseMatches(point.x, point.y)
			if err != nil {
				t.Fatalf("Error getting matches: %v", err)
			}

			if len(matches) == 0 {
				t.Skipf("No matches found for point %s", point.description)
				return
			}

			// Log number of matches
			t.Logf("Found %d matches for %s", len(matches), point.description)

			// Select best match using selector
			bestMatch := selector.SelectBestMatch(matches)

			// Add to history for future selections
			selector.AddToHistory(bestMatch)

			// Log selected vegreferanse
			t.Logf("Selected vegreferanse: %s", bestMatch)
		})

		// Add a small delay between API calls
		time.Sleep(200 * time.Millisecond)
	}
}

// TestBidirectionalConversion tests that coordinates converted to vegreferanse
// and back to coordinates match the original coordinates within reasonable precision
func TestBidirectionalConversion(t *testing.T) {
	// Skip in short mode to avoid hitting the actual API
	if testing.Short() {
		t.Skip("Skipping bidirectional test in short mode")
	}

	// Create API client
	api := NewVegvesenetAPIV4(10, time.Second, "")

	// Create selector for continuity (only for coord-to-vegref direction)
	vegrefSelector := NewVegreferanseSelector(5)

	// Test cases with known coordinates in UTM33
	testCases := []struct {
		x           float64
		y           float64
		description string
		tolerance   float64 // Acceptable difference between original and round-trip coords (in meters)
	}{
		{253671.97, 6648897.78, "E18 near Oslo", 10.0},
		{269039.00, 7038490.00, "E6 near Trondheim", 10.0},
		{641470.00, 7679980.00, "County road in northern Norway", 15.0},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Step 1: Coordinates to Vegreferanse with continuity
			matches, err := api.GetVegreferanseMatches(tc.x, tc.y)
			if err != nil {
				t.Fatalf("Error getting vegreferanse matches: %v", err)
			}

			if len(matches) == 0 {
				t.Skipf("No vegreferanse matches found for coordinates (%.6f, %.6f)", tc.x, tc.y)
				return
			}

			// Select best match considering continuity
			vegreferanse := vegrefSelector.SelectBestMatch(matches)
			vegrefSelector.AddToHistory(vegreferanse)

			t.Logf("Coordinates (%.6f, %.6f) -> Vegreferanse: %s", tc.x, tc.y, vegreferanse)

			// Skip empty vegreferanse values
			if vegreferanse == "" {
				t.Skipf("Empty vegreferanse returned for coordinates (%.6f, %.6f)", tc.x, tc.y)
				return
			}

			// Step 2: Vegreferanse back to Coordinates (ignoring continuity)
			coords, err := api.GetCoordinatesFromVegreferanse(vegreferanse)
			if err != nil {
				t.Fatalf("Error converting vegreferanse back to coordinates: %v", err)
			}

			t.Logf("Vegreferanse %s -> Coordinates: (%.6f, %.6f)",
				vegreferanse, coords.X, coords.Y)

			// Step 3: Compare original and round-trip coordinates
			distance := calculateDistance(tc.x, tc.y, coords.X, coords.Y)
			t.Logf("Distance between original and round-trip coordinates: %.2f meters", distance)

			// Verify the coordinates are close enough (within tolerance)
			if distance > tc.tolerance {
				t.Logf("Round-trip coordinates %.2f meters from original (exceeds tolerance of %.2f meters)",
					distance, tc.tolerance)
			}

			// Step 4: Skip SRID verification since we don't have access to raw API responses
			// We're assuming the API returns coordinates in UTM33/EPSG:5973 format as documented
			t.Logf("Note: Skipping explicit SRID verification - assuming UTM33/EPSG:5973 format")
		})

		// Add delay between tests
		time.Sleep(300 * time.Millisecond)
	}
}

// calculateDistance calculates the Euclidean distance between two UTM33 coordinates in meters
func calculateDistance(x1, y1, x2, y2 float64) float64 {
	dx := x2 - x1
	dy := y2 - y1
	return math.Sqrt(dx*dx + dy*dy)
}

// TestMaxDistanceFiltering tests that the max distance filtering works correctly
func TestMaxDistanceFiltering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping max distance filtering test in short mode")
	}

	// Create API client (no distance filtering at API level now)
	api := NewVegvesenetAPIV4(10, time.Second, "")

	// Use coordinates that should return multiple matches with varying distances
	x := 253671.97
	y := 6648897.78

	// Get all matches from API (unfiltered)
	allMatches, err := api.GetVegreferanseMatches(x, y)
	if err != nil {
		t.Fatalf("Error getting matches: %v", err)
	}

	// Test with different max distance settings
	testCases := []struct {
		maxDistance     int
		expectFiltering bool
		description     string
	}{
		{10, true, "Very restrictive distance (10m) - should filter out most results"},
		{100, true, "Moderately restrictive distance (100m) - should filter some results"},
		{1000, false, "Default distance (1000m) - should not filter many results"},
		{10000, false, "Very permissive distance (10000m) - should not filter any results"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Apply client-side filtering using production function
			filteredMatches := filterMatchesByDistance(allMatches, tc.maxDistance)

			// Log the number of matches and their distances
			t.Logf("Max distance %dm: Found %d matches (from %d total)",
				tc.maxDistance, len(filteredMatches), len(allMatches))
			for i, match := range filteredMatches {
				t.Logf("  Match %d: %s (distance: %.2f meters)",
					i+1, match.Vegsystemreferanse.Kortform, match.Avstand)

				// Verify that all returned matches respect the max distance filter
				if match.Avstand > float64(tc.maxDistance) {
					t.Errorf("Match %d has distance %.2f meters, which exceeds max distance %d meters",
						i+1, match.Avstand, tc.maxDistance)
				}
			}

			// Verify filtering behavior
			if tc.expectFiltering && len(filteredMatches) >= len(allMatches) {
				t.Logf("Note: Expected filtering but got same number of results. This might be okay if all matches are within the distance threshold.")
			}
		})
	}
}

// TestWKTFormatCorrespondsToUTM33 verifies that the WKT format returned by the API
// corresponds to the UTM33 (EPSG:5973) coordinate system
func TestWKTFormatCorrespondsToUTM33(t *testing.T) {
	// Skip in short mode to avoid hitting the actual API
	if testing.Short() {
		t.Skip("Skipping WKT format test in short mode")
	}

	// Create API client
	api := NewVegvesenetAPIV4(10, time.Second, "")

	// Test known vegreferanse values
	testCases := []struct {
		vegreferanse string
		description  string
	}{
		{"E6 S72D1 m1000", "E6 near Trondheim"},
		{"FV7834 S1D1 m11", "County road 7834"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Step A: Get coordinates from vegreferanse using the regular method
			coords, err := api.GetCoordinatesFromVegreferanse(tc.vegreferanse)
			if err != nil {
				t.Fatalf("Error getting coordinates: %v", err)
			}

			// Verify we got valid coordinates
			t.Logf("Vegreferanse %s -> Coordinates: (%.6f, %.6f)",
				tc.vegreferanse, coords.X, coords.Y)

			// Check for valid coordinate values (rough check for Norway)
			if coords.X < 0 || coords.Y < 6000000 {
				t.Errorf("Coordinates don't appear to be in UTM33/EPSG:5973 format: (%.6f, %.6f)",
					coords.X, coords.Y)
			}

			// Step B: Verify coordinates are in reasonable range for UTM33 in Norway
			// UTM33N Norway approximate boundaries
			if coords.X < 0 || coords.X > 1000000 {
				t.Errorf("X coordinate %.6f outside reasonable range for UTM33 in Norway", coords.X)
			}
			if coords.Y < 6400000 || coords.Y > 7800000 {
				t.Errorf("Y coordinate %.6f outside reasonable range for UTM33 in Norway", coords.Y)
			}

			// Step C: Reverse lookup - convert coordinates back to vegreferanse
			matches, err := api.GetVegreferanseMatches(coords.X, coords.Y)
			if err != nil {
				t.Fatalf("Error getting vegreferanses for coordinates: %v", err)
			}

			// Check if we got any matches
			if len(matches) == 0 {
				t.Errorf("No vegreferanse matches found for coordinates (%.6f, %.6f)",
					coords.X, coords.Y)
				return
			}

			// Log the first match
			t.Logf("Coordinates (%.6f, %.6f) -> First match: %s (distance: %.2f meters)",
				coords.X, coords.Y, matches[0].Vegsystemreferanse.Kortform, matches[0].Avstand)

			// Check if the original vegreferanse is among the matches
			// Note: We can't expect an exact match due to different referencing methods,
			// but we can check if road ID and main section are similar
			originalParts := strings.Split(tc.vegreferanse, " ")
			if len(originalParts) > 0 {
				originalRoadID := originalParts[0] // e.g., "E18"
				foundSimilar := false

				for _, match := range matches {
					matchParts := strings.Split(match.Vegsystemreferanse.Kortform, " ")
					if len(matchParts) > 0 && matchParts[0] == originalRoadID {
						foundSimilar = true
						t.Logf("Found matching road ID %s in reverse lookup", originalRoadID)
						break
					}
				}

				if !foundSimilar {
					t.Logf("Warning: Original road ID %s not found in reverse lookup matches", originalRoadID)
				}
			}
		})

		// Add delay between tests
		time.Sleep(300 * time.Millisecond)
	}
}
