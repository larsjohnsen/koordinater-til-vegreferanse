package main

import (
	"os"
	"path/filepath"
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
	err = processFile(inputPath, outputPath, apiClient, 1, 4, 5)
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
