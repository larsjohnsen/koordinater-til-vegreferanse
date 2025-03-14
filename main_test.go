package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestProcessFile tests the file processing functionality with the actual API
func TestProcessFile(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "vegreferanse-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test input file
	inputPath := filepath.Join(tempDir, "test_input.txt")
	inputContent := "Header1\tHeader2\tHeader3\tHeader4\tX\tY\n" +
		"data1\tdata2\tdata3\tdata4\t600000\t6600000\n" +
		"data5\tdata6\tdata7\tdata8\t600001\t6600001\n"
	err = os.WriteFile(inputPath, []byte(inputContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test input file: %v", err)
	}

	// Set up the output path
	outputPath := filepath.Join(tempDir, "test_output.txt")

	// Create a properly initialized API client
	// Parameters: rate limit (10 calls per minute), search radius (20 meters), no disk cache
	apiClient := NewVegvesenetAPIV4(10, time.Minute, 20, "")

	// Process the file using the actual API client with 1 worker (sequential processing for testing)
	err = processFile(inputPath, outputPath, apiClient, Config{
		Mode: "coord_to_vegref",
		CoordToVegref: &CoordToVegrefConfig{
			XColumn: 4,
			YColumn: 5,
		},
	})
	if err != nil {
		t.Fatalf("Failed to process file: %v", err)
	}

	// Read the output file
	outputContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	// Check that the output file has content and the expected header
	expectedHeader := "Header1\tHeader2\tHeader3\tHeader4\tX\tY\tVegreferanse\n"
	if len(outputContent) == 0 || string(outputContent[:len(expectedHeader)]) != expectedHeader {
		t.Errorf("Output file has wrong header or is empty. Got: %s", string(outputContent))
	}

	// Log the actual output content for inspection
	t.Logf("Output content: %s", string(outputContent))
}

// TestProcessVegreferanseToCoordinates tests the vegreferanse to coordinates processing
func TestProcessVegreferanseToCoordinates(t *testing.T) {
	// Skip in short mode to avoid hitting the actual API
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Create test input lines with vegreferanse values
	lines := []string{
		"data1\tdata2\tdata3\tFV7834 S1D1 m11",
		"data4\tdata5\tdata6\tFV7834 S1D1 m12",
		// Add an invalid vegreferanse to test error handling
		"data7\tdata8\tdata9\tINVALID_VEGREF",
	}

	// Create API client
	apiClient := NewVegvesenetAPIV4(10, time.Second, 20, "")

	// Test configuration
	config := VegrefToCoordConfig{
		VegreferanseColumn: 3, // 0-based index of vegreferanse column
	}

	// Process the test data
	results, err := processVegreferanseToCoordinates(lines, apiClient, 1, config)
	if err != nil {
		t.Fatalf("Failed to process vegreferanse to coordinates: %v", err)
	}

	// Verify the results
	t.Logf("Processed %d lines", len(results))

	// Check the successful conversions
	for i, result := range results {
		t.Logf("Result for line %d: %v", i, result)

		// Skip lines with errors
		if result.err != nil {
			t.Logf("Line %d had error: %v", i, result.err)
			continue
		}

		// Verify the format of the result
		// The result.vegreferanse field should contain tab-separated X and Y coordinates
		coords := strings.Split(result.vegreferanse, "\t")
		if len(coords) != 2 {
			t.Errorf("Line %d: Expected 2 coordinates, got %d: %s", i, len(coords), result.vegreferanse)
			continue
		}

		// Try to parse the coordinates
		x, xErr := strconv.ParseFloat(coords[0], 64)
		y, yErr := strconv.ParseFloat(coords[1], 64)

		if xErr != nil || yErr != nil {
			t.Errorf("Line %d: Failed to parse coordinates: %v, %v", i, xErr, yErr)
			continue
		}

		// Verify the coordinates are in a reasonable range for Norway
		if x < 0 || x > 1000000 {
			t.Errorf("Line %d: X coordinate %.6f is outside reasonable range for Norway", i, x)
		}
		if y < 6400000 || y > 7800000 {
			t.Errorf("Line %d: Y coordinate %.6f is outside reasonable range for Norway", i, y)
		}
	}
}

// TestProcessFileVegrefToCoord tests the entire vegref_to_coord mode
func TestProcessFileVegrefToCoord(t *testing.T) {
	// Skip in short mode to avoid hitting the actual API
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "vegref-to-coord-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test input file with vegreferanse values
	inputPath := filepath.Join(tempDir, "vegref_input.txt")
	inputContent := "Header1\tHeader2\tHeader3\tVegreferanse\n" +
		"data1\tdata2\tdata3\tFV7834 S1D1 m11\n" +
		"data4\tdata5\tdata6\tFV7834 S1D1 m12\n"
	err = os.WriteFile(inputPath, []byte(inputContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test input file: %v", err)
	}

	// Set up the output path
	outputPath := filepath.Join(tempDir, "vegref_output.txt")

	// Create a properly initialized API client
	apiClient := NewVegvesenetAPIV4(10, time.Second, 20, "")

	// Process the file using the actual API client
	err = processFile(inputPath, outputPath, apiClient, Config{
		Mode: "vegref_to_coord",
		VegrefToCoord: &VegrefToCoordConfig{
			VegreferanseColumn: 3, // 0-based index of vegreferanse column
		},
		Workers: 1, // Use 1 worker for predictable sequential processing
	})
	if err != nil {
		t.Fatalf("Failed to process file: %v", err)
	}

	// Read the output file
	outputContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	// Check that the output file has content and the expected header
	expectedHeader := "Header1\tHeader2\tHeader3\tVegreferanse\tX_UTM33\tY_UTM33\n"
	if len(outputContent) == 0 || string(outputContent[:len(expectedHeader)]) != expectedHeader {
		t.Errorf("Output file has wrong header or is empty. Got: %s", string(outputContent))
	}

	// Log the actual output content for inspection
	t.Logf("Output content: %s", string(outputContent))

	// Check each line of the output file
	lines := strings.Split(string(outputContent), "\n")
	if len(lines) < 3 { // Header + 2 data lines
		t.Fatalf("Expected at least 3 lines in output file, got %d", len(lines))
	}

	// Check the data lines
	for i := 1; i < len(lines)-1; i++ { // Skip header and last empty line
		fields := strings.Split(lines[i], "\t")
		if len(fields) < 6 { // Original 4 columns + 2 coordinate columns
			t.Errorf("Line %d has %d fields, expected at least 6: %s", i, len(fields), lines[i])
			continue
		}

		// Try to parse X and Y coordinates
		x, xErr := strconv.ParseFloat(fields[4], 64)
		y, yErr := strconv.ParseFloat(fields[5], 64)

		if xErr != nil || yErr != nil {
			t.Errorf("Line %d: Failed to parse coordinates: %v, %v", i, xErr, yErr)
			continue
		}

		// Verify coordinates are in a reasonable range for Norway
		if x < 0 || x > 1000000 {
			t.Errorf("Line %d: X coordinate %.6f is outside reasonable range for Norway", i, x)
		}
		if y < 6400000 || y > 7800000 {
			t.Errorf("Line %d: Y coordinate %.6f is outside reasonable range for Norway", i, y)
		}

		// Log the coordinates
		t.Logf("Line %d: Vegreferanse %s -> Coordinates: X=%.6f, Y=%.6f",
			i, fields[3], x, y)
	}
}
