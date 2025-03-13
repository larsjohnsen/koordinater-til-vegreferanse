package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestVegvesenetAPIV4_Comprehensive combines all the tests for the API client
func TestVegvesenetAPIV4_Comprehensive(t *testing.T) {
	// Basic functionality test
	t.Run("BasicFunctionality", func(t *testing.T) {
		// Create API client with small cache and rate limiter
		api := NewVegvesenetAPIV4(10, time.Minute, 20, "")

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
		api := NewVegvesenetAPIV4(10, time.Minute, 10, "")

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
		apiClient := NewVegvesenetAPIV4(10, time.Second, 15, "")

		// Test the raw API response format
		t.Run("DiagnoseRawResponse", func(t *testing.T) {
			// Choose a known coordinate pair in Norway (E18 near Oslo)
			// These coordinates are in UTM33 EUREF89 (EPSG:5973)
			x := 253671.97
			y := 6648897.78

			// Use the helper function to get the raw API response
			response, err := apiClient.LogAPIResponse(x, y)
			if err != nil {
				t.Fatalf("Error making diagnostic request: %v", err)
			}

			// Log the full response for inspection
			t.Logf("API Response for coordinates (%.6f, %.6f):\n%s", x, y, response)

			// Verify we got a response (not checking content yet)
			if !strings.Contains(response, "Status: 200") {
				t.Errorf("Response doesn't appear to be valid")
			}
		})

		// Test with various coordinates
		t.Run("MultipleLocations", func(t *testing.T) {
			// Define test cases with known coordinates
			testCases := []struct {
				x           float64
				y           float64
				description string
				expectEmpty bool
			}{
				{253671.97, 6648897.78, "E18 near Oslo", false},
				{269039.00, 7038490.00, "E6 near Trondheim", false},
				{201272.00, 6878247.00, "Highway in western Norway", true},
				{0.0, 0.0, "Invalid coordinates (ocean)", true}, // Should return empty string (no road)
			}

			for _, tc := range testCases {
				t.Run(tc.description, func(t *testing.T) {
					vegreferanse, err := apiClient.GetVegreferanseFromCoordinates(tc.x, tc.y)

					// Check error (should be nil even for empty results)
					if err != nil {
						t.Fatalf("Error getting vegreferanse: %v", err)
					}

					// Log the result
					t.Logf("Coordinates (%.6f, %.6f) -> Vegreferanse: %s",
						tc.x, tc.y, vegreferanse)

					// Check if result matches expectations
					if tc.expectEmpty && vegreferanse != "" {
						t.Errorf("Expected empty vegreferanse for %s, got %s",
							tc.description, vegreferanse)
					} else if !tc.expectEmpty && vegreferanse == "" {
						t.Errorf("Expected non-empty vegreferanse for %s, got empty string",
							tc.description)
					}

					// Validate format (if not expecting empty)
					if !tc.expectEmpty && len(vegreferanse) < 3 {
						t.Errorf("Vegreferanse format looks invalid: %s", vegreferanse)
					}
				})

				// Add a small delay between tests to respect rate limits
				time.Sleep(200 * time.Millisecond)
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

		api := NewVegvesenetAPIV4(10, time.Second, 30, "") // Use larger radius to find multiple matches

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

	api := NewVegvesenetAPIV4(10, time.Second, 15, "")
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
