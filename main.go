// Koordinater til Vegreferanse
//
// This program converts UTM33 coordinates to Norwegian road references (vegreferanse)
// using the Norwegian Public Roads Administration (NVDB) API v4.
//
// Features:
// - Converts UTM33 coordinates to vegreferanse using the NVDB API v4
// - Intelligent road selection that maintains travel continuity when multiple road matches are available
// - Efficient disk-based caching system to reduce API calls and speed up processing
// - Configurable API rate limiting to comply with NVDB's usage policies
// - Parallel processing with configurable number of workers
// - Processes tab-delimited input files containing coordinate data
//
// The main component in this file handles:
// - File I/O operations
// - Command-line flag processing
// - Coordination of the conversion process
// - Worker management for parallel processing

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
)

// Config holds all program configuration settings
type Config struct {
	// Mode settings
	Mode string `validate:"required,oneof=coord_to_vegref vegref_to_coord"`

	// File paths
	InputPath  string `validate:"required,fileexists"`
	OutputPath string `validate:"required,outputdirexists"`

	// Cache settings
	DisableCache bool
	CacheDir     string
	ClearCache   bool

	// API settings
	RateLimit     int `validate:"min=1,max=1000"`
	RateLimitTime int `validate:"min=1,max=10000"`

	// Processing settings
	Workers int `validate:"min=1,max=100"`

	// Mode-specific configurations (only one will be populated based on the mode)
	CoordToVegref *CoordToVegrefConfig `validate:"required_if=Mode coord_to_vegref"`
	VegrefToCoord *VegrefToCoordConfig `validate:"required_if=Mode vegref_to_coord"`
}

// CoordToVegrefConfig holds configuration specific to coordinates to vegreferanse mode
type CoordToVegrefConfig struct {
	XColumn int `validate:"min=0"`
	YColumn int `validate:"min=0"`
}

// VegrefToCoordConfig holds configuration specific to vegreferanse to coordinates mode
type VegrefToCoordConfig struct {
	VegreferanseColumn int `validate:"min=0"`
}

// Coordinate represents a geographical coordinate point
type Coordinate struct {
	X float64 // Easting (X)
	Y float64 // Northing (Y)
}

// VegreferanseProvider defines the interface for services that can convert coordinates to vegreferanse
type VegreferanseProvider interface {
	// GetVegreferanseFromCoordinates converts UTM33 coordinates to a vegreferanse string
	GetVegreferanseFromCoordinates(x, y float64) (string, error)

	// GetVegreferanseMatches returns all matching vegreferanses for the given coordinates
	GetVegreferanseMatches(x, y float64) ([]VegreferanseMatch, error)
}

// CoordinateProvider defines the interface for services that can convert vegreferanse to coordinates
type CoordinateProvider interface {
	// GetCoordinatesFromVegreferanse converts a vegreferanse string to UTM33 coordinates
	GetCoordinatesFromVegreferanse(vegreferanse string) (Coordinate, error)
}

// RateLimiter handles API rate limiting
type RateLimiter struct {
	calls     []time.Time
	limit     int
	timeFrame time.Duration
	mu        sync.Mutex
}

// processTask represents a single line to be processed
type processTask struct {
	lineIdx int
	line    string
}

// processResult represents the result of processing a single line
type processResult struct {
	lineIdx      int
	line         string
	vegreferanse string
	matches      []VegreferanseMatch
	err          error
}

// roadRange represents a continuous range of rows for a specific road
type roadRange struct {
	startRow int
	endRow   int
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, timeFrame time.Duration) *RateLimiter {
	return &RateLimiter{
		calls:     make([]time.Time, 0, limit),
		limit:     limit,
		timeFrame: timeFrame,
	}
}

// Wait blocks until a new API call is allowed
func (r *RateLimiter) Wait() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Remove timestamps older than the time frame
	validCalls := make([]time.Time, 0, len(r.calls))
	for _, call := range r.calls {
		if now.Sub(call) < r.timeFrame {
			validCalls = append(validCalls, call)
		}
	}
	r.calls = validCalls

	// If we've reached the limit, wait until we can make a new call
	if len(r.calls) >= r.limit {
		oldest := r.calls[0]
		waitTime := r.timeFrame - now.Sub(oldest)
		if waitTime > 0 {
			time.Sleep(waitTime)
			now = time.Now()

			// Re-filter calls after waiting since more might have expired
			validCalls = make([]time.Time, 0, len(r.calls))
			for _, call := range r.calls {
				if now.Sub(call) < r.timeFrame {
					validCalls = append(validCalls, call)
				}
			}
			r.calls = validCalls
		}
	}

	// Add the new call time
	r.calls = append(r.calls, now)
}

// Helper function to get maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// extractRoadNumber extracts the road number (e.g., "E18", "Rv4") from a vegreferanse string
func extractRoadNumber(vegreferanse string) string {
	if vegreferanse == "" {
		return ""
	}

	// Split by space and take the first part (e.g., "E18" from "E18 S65D1 m12621")
	parts := strings.Fields(vegreferanse)
	if len(parts) == 0 {
		return ""
	}

	return parts[0]
}

// validateFileExists validates that a file exists
func validateFileExists(fl validator.FieldLevel) bool {
	path := fl.Field().String()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

// validateOutputDirExists validates that the directory of the output file exists
func validateOutputDirExists(fl validator.FieldLevel) bool {
	path := fl.Field().String()
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return false
	}
	return true
}

// parseConfig parses command-line flags and returns a Config struct
func parseConfig() (Config, error) {
	var config Config

	// Variables to store flag values temporarily until we know which mode-specific config to create
	var xColumn, yColumn, vegreferanseColumn int

	// Define common flags
	flag.StringVar(&config.Mode, "mode", "", "Conversion mode: coord_to_vegref or vegref_to_coord (required)")
	flag.StringVar(&config.InputPath, "input", "", "Input file path (required)")
	flag.StringVar(&config.OutputPath, "output", "", "Output file path (required)")
	flag.BoolVar(&config.DisableCache, "no-cache", false, "Disable disk cache")
	flag.StringVar(&config.CacheDir, "cache-dir", "cache/api_responses", "Directory for disk cache")
	flag.BoolVar(&config.ClearCache, "clear-cache", false, "Clear existing cache before starting")
	flag.IntVar(&config.RateLimit, "rate-limit", 40, "Number of API calls allowed per time frame (NVDB default: 40)")
	flag.IntVar(&config.RateLimitTime, "rate-time", 1000, "Rate limit time frame in milliseconds (NVDB default: 1000)")
	flag.IntVar(&config.Workers, "workers", 5, "Number of concurrent workers")

	// Mode-specific flags - use temporary variables
	flag.IntVar(&xColumn, "x-column", -1, "0-based index of the column containing X coordinates (required for coord_to_vegref mode)")
	flag.IntVar(&yColumn, "y-column", -1, "0-based index of the column containing Y coordinates (required for coord_to_vegref mode)")
	flag.IntVar(&vegreferanseColumn, "vegreferanse-column", -1, "0-based index of the column containing vegreferanse (required for vegref_to_coord mode)")

	flag.Parse()

	// Create the appropriate mode-specific configuration based on mode
	switch config.Mode {
	case "coord_to_vegref":
		config.CoordToVegref = &CoordToVegrefConfig{
			XColumn: xColumn,
			YColumn: yColumn,
		}
	case "vegref_to_coord":
		config.VegrefToCoord = &VegrefToCoordConfig{
			VegreferanseColumn: vegreferanseColumn,
		}
	}

	// Initialize validator
	validate := validator.New()

	// Register custom validation functions
	validate.RegisterValidation("fileexists", validateFileExists)
	validate.RegisterValidation("outputdirexists", validateOutputDirExists)

	// Validate the configuration
	if err := validate.Struct(config); err != nil {
		validationErrors := err.(validator.ValidationErrors)
		for _, e := range validationErrors {
			switch e.Field() {
			case "Mode":
				return config, fmt.Errorf("invalid mode: %s, must be either coord_to_vegref or vegref_to_coord", config.Mode)
			case "InputPath":
				if e.Tag() == "required" {
					return config, fmt.Errorf("input file path is required: use -input=<file>")
				} else if e.Tag() == "fileexists" {
					return config, fmt.Errorf("input file does not exist: %s", config.InputPath)
				}
			case "OutputPath":
				if e.Tag() == "required" {
					return config, fmt.Errorf("output file path is required: use -output=<file>")
				} else if e.Tag() == "outputdirexists" {
					return config, fmt.Errorf("output directory does not exist: %s", filepath.Dir(config.OutputPath))
				}
			case "CoordToVegref":
				return config, fmt.Errorf("coord_to_vegref configuration is required for coord_to_vegref mode")
			case "VegrefToCoord":
				return config, fmt.Errorf("vegref_to_coord configuration is required for vegref_to_coord mode")
			default:
				return config, fmt.Errorf("invalid value for %s: %v", e.Field(), e.Value())
			}
		}
		return config, err
	}

	return config, nil
}

// setupCache initializes and configures the disk cache
func setupCache(config Config) string {
	if config.DisableCache {
		return ""
	}

	cacheDirPath := config.CacheDir
	if err := os.MkdirAll(cacheDirPath, 0755); err != nil {
		fmt.Printf("Warning: Failed to create cache directory: %v\n", err)
		return "" // Disable disk cache if we can't create the directory
	}

	if config.ClearCache {
		// Clear cache if requested
		dc, err := NewVegreferanseDiskCache(cacheDirPath)
		if err != nil {
			fmt.Printf("Warning: Failed to initialize disk cache: %v\n", err)
		} else {
			fmt.Println("Clearing disk cache...")
			if err := dc.Clear(); err != nil {
				fmt.Printf("Warning: Failed to clear cache: %v\n", err)
			} else {
				// Recreate the directory after clearing
				_ = os.MkdirAll(cacheDirPath, 0755)
				fmt.Println("Cache cleared successfully.")
			}
		}
	}

	return cacheDirPath
}

// readInputFile reads the input file and returns header and data lines
func readInputFile(inputPath string, config Config) (string, []string, error) {
	// Open input file
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to open input file: %w", err)
	}
	defer inputFile.Close()

	scanner := bufio.NewScanner(inputFile)

	// Process header
	var header string
	if !scanner.Scan() {
		return "", nil, fmt.Errorf("input file is empty")
	}
	header = scanner.Text()

	// Verify columns in header
	headerColumns := strings.Split(header, "\t")
	expectedColumnCount := len(headerColumns)

	// Validate column indices based on mode
	switch config.Mode {
	case "coord_to_vegref":
		if config.CoordToVegref == nil {
			return "", nil, fmt.Errorf("coord_to_vegref configuration is not initialized")
		}

		// Validate X and Y column indices
		if config.CoordToVegref.XColumn < 0 || config.CoordToVegref.XColumn >= expectedColumnCount {
			return "", nil, fmt.Errorf("column X index %d is out of range (file has %d columns)",
				config.CoordToVegref.XColumn, expectedColumnCount)
		}
		if config.CoordToVegref.YColumn < 0 || config.CoordToVegref.YColumn >= expectedColumnCount {
			return "", nil, fmt.Errorf("column Y index %d is out of range (file has %d columns)",
				config.CoordToVegref.YColumn, expectedColumnCount)
		}
		fmt.Printf("Input file has %d columns. Using column %d for X and column %d for Y coordinates\n",
			expectedColumnCount, config.CoordToVegref.XColumn, config.CoordToVegref.YColumn)

	case "vegref_to_coord":
		if config.VegrefToCoord == nil {
			return "", nil, fmt.Errorf("vegref_to_coord configuration is not initialized")
		}

		// Validate vegreferanse column index
		if config.VegrefToCoord.VegreferanseColumn < 0 || config.VegrefToCoord.VegreferanseColumn >= expectedColumnCount {
			return "", nil, fmt.Errorf("column Vegreferanse index %d is out of range (file has %d columns)",
				config.VegrefToCoord.VegreferanseColumn, expectedColumnCount)
		}
		fmt.Printf("Input file has %d columns. Using column %d for Vegreferanse\n",
			expectedColumnCount, config.VegrefToCoord.VegreferanseColumn)
	}

	// Read all data lines into memory
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return "", nil, fmt.Errorf("error reading input file: %w", err)
	}

	fmt.Printf("Read %d lines from file\n", len(lines)+1) // +1 for header

	return header, lines, nil
}

// processCoordinatesToVegreferanse processes the input file to convert coordinates to vegreferanse
func processCoordinatesToVegreferanse(lines []string, provider VegreferanseProvider, workers int, modeConfig CoordToVegrefConfig) ([]processResult, error) {
	// Create a channel for tasks and results with buffering
	taskChannel := make(chan processTask, len(lines))
	resultChannel := make(chan processResult, len(lines))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChannel {
				line := task.line
				lineIdx := task.lineIdx

				// Split the line by tabs
				fields := strings.Split(line, "\t")

				// Skip lines that don't have enough columns for coordinates
				if len(fields) <= max(modeConfig.XColumn, modeConfig.YColumn) {
					resultChannel <- processResult{
						lineIdx: lineIdx,
						line:    line,
						err:     fmt.Errorf("line doesn't have enough columns for coordinates"),
					}
					continue
				}

				// Parse X and Y coordinates
				x, err := strconv.ParseFloat(fields[modeConfig.XColumn], 64)
				if err != nil {
					resultChannel <- processResult{
						lineIdx: lineIdx,
						line:    line,
						err:     fmt.Errorf("invalid X coordinate: %v", err),
					}
					continue
				}

				y, err := strconv.ParseFloat(fields[modeConfig.YColumn], 64)
				if err != nil {
					resultChannel <- processResult{
						lineIdx: lineIdx,
						line:    line,
						err:     fmt.Errorf("invalid Y coordinate: %v", err),
					}
					continue
				}

				// Get all matches for this coordinate
				matches, err := provider.GetVegreferanseMatches(x, y)
				if err != nil {
					resultChannel <- processResult{
						lineIdx: lineIdx,
						line:    line,
						err:     fmt.Errorf("API error: %v", err),
					}
					continue
				}

				// Default to empty string if no matches were found
				vegreferanse := ""
				if len(matches) > 0 {
					// Get the first match by default - the selector will improve this
					vegreferanse = matches[0].Vegsystemreferanse.Kortform
				}

				resultChannel <- processResult{
					lineIdx:      lineIdx,
					line:         line,
					vegreferanse: vegreferanse,
					matches:      matches,
				}
			}
		}()
	}

	// Queue all tasks
	for i, line := range lines {
		taskChannel <- processTask{
			lineIdx: i,
			line:    line,
		}
	}
	close(taskChannel)

	// Wait for all workers to finish
	wg.Wait()
	close(resultChannel)

	// Collect results
	results := make([]processResult, len(lines))
	for result := range resultChannel {
		results[result.lineIdx] = result
	}

	// Sort results by lineIdx
	sort.Slice(results, func(i, j int) bool {
		return results[i].lineIdx < results[j].lineIdx
	})

	return results, nil
}

// processVegreferanseToCoordinates processes the input file to convert vegreferanse to coordinates
func processVegreferanseToCoordinates(lines []string, provider CoordinateProvider, workers int, modeConfig VegrefToCoordConfig) ([]processResult, error) {
	// Create a channel for tasks and results with buffering
	taskChannel := make(chan processTask, len(lines))
	resultChannel := make(chan processResult, len(lines))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChannel {
				line := task.line
				lineIdx := task.lineIdx

				// Split the line by tabs
				fields := strings.Split(line, "\t")

				// Skip lines that don't have enough columns for vegreferanse
				if len(fields) <= modeConfig.VegreferanseColumn {
					resultChannel <- processResult{
						lineIdx: lineIdx,
						line:    line,
						err:     fmt.Errorf("line doesn't have enough columns for vegreferanse"),
					}
					continue
				}

				// Get vegreferanse from the specified column
				vegreferanse := strings.TrimSpace(fields[modeConfig.VegreferanseColumn])
				if vegreferanse == "" {
					resultChannel <- processResult{
						lineIdx: lineIdx,
						line:    line,
						err:     fmt.Errorf("empty vegreferanse"),
					}
					continue
				}

				// Get coordinates for this vegreferanse
				coords, err := provider.GetCoordinatesFromVegreferanse(vegreferanse)
				if err != nil {
					resultChannel <- processResult{
						lineIdx: lineIdx,
						line:    line,
						err:     fmt.Errorf("API error: %v", err),
					}
					continue
				}

				// Format the result - the original line will have the coordinates appended
				xValue := fmt.Sprintf("%.6f", coords.X)
				yValue := fmt.Sprintf("%.6f", coords.Y)

				// Create a modified line with X and Y coordinates
				resultChannel <- processResult{
					lineIdx:      lineIdx,
					line:         line,
					vegreferanse: fmt.Sprintf("%s\t%s", xValue, yValue), // Using vegreferanse field to store X and Y for compatibility
				}
			}
		}()
	}

	// Queue all tasks
	for i, line := range lines {
		taskChannel <- processTask{
			lineIdx: i,
			line:    line,
		}
	}
	close(taskChannel)

	// Wait for all workers to finish
	wg.Wait()
	close(resultChannel)

	// Collect results
	results := make([]processResult, len(lines))
	for result := range resultChannel {
		results[result.lineIdx] = result
	}

	// Sort results by lineIdx
	sort.Slice(results, func(i, j int) bool {
		return results[i].lineIdx < results[j].lineIdx
	})

	return results, nil
}

// applyVegreferanseSelector applies the road continuity selection to results
func applyVegreferanseSelector(results []processResult) {
	selector := NewVegreferanseSelector(10) // Keep track of last 10 vegreferanses

	// Apply selector in sequential order
	for i := range results {
		result := &results[i]
		if len(result.matches) > 0 {
			result.vegreferanse = selector.SelectBestMatch(result.matches)
			selector.AddToHistory(result.vegreferanse)
		}
	}
}

// identifyRoadRanges identifies the ranges of rows for each road number
func identifyRoadRanges(results []processResult) map[string][]roadRange {
	roadNumbers := make(map[string][]roadRange)
	currentRoad := ""
	currentRange := roadRange{}

	// First identify all the road number ranges
	for i, result := range results {
		roadNumber := extractRoadNumber(result.vegreferanse)

		// Skip empty road numbers
		if roadNumber == "" {
			// If we were tracking a road, finish the current range
			if currentRoad != "" {
				currentRange.endRow = i
				roadNumbers[currentRoad] = append(roadNumbers[currentRoad], currentRange)
				currentRoad = ""
			}
			continue
		}

		// If this is a new road or the first road number
		if roadNumber != currentRoad {
			// If we were tracking a road, finish the current range
			if currentRoad != "" {
				currentRange.endRow = i
				roadNumbers[currentRoad] = append(roadNumbers[currentRoad], currentRange)
			}

			// Start a new range
			currentRoad = roadNumber
			currentRange = roadRange{startRow: i + 1} // +1 because we want 1-indexed row numbers for display
		} else {
			// Same road, continue the current range
			// We'll update the end row at the end or when the road changes
			currentRange.endRow = i + 1
		}
	}

	// Handle the last range if there was one
	if currentRoad != "" {
		currentRange.endRow = len(results)
		roadNumbers[currentRoad] = append(roadNumbers[currentRoad], currentRange)
	}

	return roadNumbers
}

// writeResults writes the processed results to the output file with mode-specific handling
func writeResults(outputPath, header string, results []processResult) (int, error) {
	// Open output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	// Create buffered writer
	writer := bufio.NewWriter(outputFile)

	// Write header
	_, err = writer.WriteString(header + "\n")
	if err != nil {
		return 0, fmt.Errorf("failed to write header: %w", err)
	}

	// Write data lines
	linesWritten := 0
	errCount := 0

	for _, result := range results {
		if result.err != nil {
			fmt.Printf("Error on line %d: %v\n", result.lineIdx+1, result.err)
			errCount++
			continue
		}

		line := result.line + "\t" + result.vegreferanse + "\n"
		_, err = writer.WriteString(line)
		if err != nil {
			return linesWritten, fmt.Errorf("failed to write line %d: %w", result.lineIdx+1, err)
		}
		linesWritten++
	}

	// Flush writer
	if err = writer.Flush(); err != nil {
		return linesWritten, fmt.Errorf("failed to flush writer: %w", err)
	}

	if errCount > 0 {
		fmt.Printf("Encountered errors on %d lines. Those lines were skipped in the output.\n", errCount)
	}

	return linesWritten, nil
}

// generateRoadReport generates and prints a report of road number ranges
func generateRoadReport(roadNumbers map[string][]roadRange) {
	fmt.Println("\nRoad numbers summary:")
	if len(roadNumbers) == 0 {
		fmt.Println("No road numbers identified.")
		return
	}

	// Get the roads in sorted order for consistent output
	roadList := make([]string, 0, len(roadNumbers))
	for road := range roadNumbers {
		roadList = append(roadList, road)
	}
	sort.Strings(roadList)

	for _, road := range roadList {
		ranges := roadNumbers[road]

		// Merge adjacent ranges for cleaner output
		if len(ranges) > 1 {
			mergedRanges := []roadRange{ranges[0]}

			for i := 1; i < len(ranges); i++ {
				lastRange := &mergedRanges[len(mergedRanges)-1]
				currentRange := ranges[i]

				// If current range starts immediately after last range ends, merge them
				if currentRange.startRow <= lastRange.endRow+1 {
					if currentRange.endRow > lastRange.endRow {
						lastRange.endRow = currentRange.endRow
					}
				} else {
					// Non-adjacent range, add as a new entry
					mergedRanges = append(mergedRanges, currentRange)
				}
			}

			ranges = mergedRanges
		}

		for _, r := range ranges {
			// Add 2 to account for:
			// 1. The header row (index 0 -> row 1)
			// 2. Converting from 0-indexed to 1-indexed
			fmt.Printf("%s - Rows %d-%d\n", road, r.startRow+1, r.endRow+1)
		}
	}
}

// processFile reads, processes, and writes the results to the output file
func processFile(inputPath, outputPath string, apiClient *VegvesenetAPIV4, config Config) error {
	// Read input file
	header, lines, err := readInputFile(inputPath, config)
	if err != nil {
		return err
	}

	// Process based on selected mode
	var results []processResult
	switch config.Mode {
	case "coord_to_vegref":
		if config.CoordToVegref == nil {
			return fmt.Errorf("coord_to_vegref configuration is not initialized")
		}

		fmt.Println("Converting coordinates to vegreferanse...")
		results, err = processCoordinatesToVegreferanse(
			lines,
			apiClient,
			config.Workers,
			*config.CoordToVegref,
		)

		if err != nil {
			return err
		}

		// Apply the vegreferanse selector to improve road matching
		applyVegreferanseSelector(results)

		// Update header to add the vegreferanse column
		header = header + "\tVegreferanse"

	case "vegref_to_coord":
		if config.VegrefToCoord == nil {
			return fmt.Errorf("vegref_to_coord configuration is not initialized")
		}

		fmt.Println("Converting vegreferanse to coordinates...")
		results, err = processVegreferanseToCoordinates(
			lines,
			apiClient,
			config.Workers,
			*config.VegrefToCoord,
		)

		if err != nil {
			return err
		}

		// Update header to add X and Y columns
		header = header + "\tX_UTM33\tY_UTM33"

	default:
		return fmt.Errorf("invalid mode: %s", config.Mode)
	}

	// Write results to output file
	linesWritten, err := writeResults(outputPath, header, results)
	if err != nil {
		return err
	}

	fmt.Printf("Processed %d lines, wrote %d lines to %s\n", len(lines), linesWritten, outputPath)

	// In coord_to_vegref mode, generate a road report
	if config.Mode == "coord_to_vegref" {
		// Identify road number ranges
		roadNumbers := identifyRoadRanges(results)
		// Generate road report
		generateRoadReport(roadNumbers)
	}

	return nil
}

func main() {
	// Set custom usage text with automatic flag generation
	flag.Usage = func() {
		// Get the program name from os.Args[0], but use just the base name for cleaner output
		progName := filepath.Base(os.Args[0])

		fmt.Fprintf(os.Stderr, "Bidirectional conversion between UTM33 coordinates and vegreferanse\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  For coord_to_vegref mode (coordinates to vegreferanse):\n")
		fmt.Fprintf(os.Stderr, "    %s -mode=coord_to_vegref -input=<file> -output=<file> -x-column=<index> -y-column=<index> [options]\n\n", progName)
		fmt.Fprintf(os.Stderr, "  For vegref_to_coord mode (vegreferanse to coordinates):\n")
		fmt.Fprintf(os.Stderr, "    %s -mode=vegref_to_coord -input=<file> -output=<file> -vegreferanse-column=<index> [options]\n\n", progName)

		// Group flags by category
		requiredFlags := []string{"mode", "input", "output"}
		modeSpecificFlags := []string{"x-column", "y-column", "vegreferanse-column"}

		// Calculate the maximum flag name length for proper alignment
		maxFlagLen := 0
		flag.VisitAll(func(f *flag.Flag) {
			if len(f.Name) > maxFlagLen {
				maxFlagLen = len(f.Name)
			}
		})
		// Add some padding
		columnWidth := maxFlagLen + 4

		// Print required flags
		fmt.Fprintf(os.Stderr, "Required flags:\n")
		flag.VisitAll(func(f *flag.Flag) {
			if contains(requiredFlags, f.Name) {
				fmt.Fprintf(os.Stderr, "  -%s%s%s\n", f.Name, getSpaces(columnWidth-len(f.Name)), f.Usage)
			}
		})
		fmt.Fprintf(os.Stderr, "\n")

		// Print mode-specific flags
		fmt.Fprintf(os.Stderr, "Mode-specific flags:\n")
		flag.VisitAll(func(f *flag.Flag) {
			if contains(modeSpecificFlags, f.Name) {
				fmt.Fprintf(os.Stderr, "  -%s%s%s\n", f.Name, getSpaces(columnWidth-len(f.Name)), f.Usage)
			}
		})
		fmt.Fprintf(os.Stderr, "\n")

		// Print optional flags
		fmt.Fprintf(os.Stderr, "Optional flags:\n")
		flag.VisitAll(func(f *flag.Flag) {
			if !contains(requiredFlags, f.Name) && !contains(modeSpecificFlags, f.Name) {
				defaultValue := f.DefValue
				if defaultValue != "" && defaultValue != "false" && defaultValue != "0" {
					fmt.Fprintf(os.Stderr, "  -%s%s%s (default: %s)\n", f.Name, getSpaces(columnWidth-len(f.Name)), f.Usage, defaultValue)
				} else {
					fmt.Fprintf(os.Stderr, "  -%s%s%s\n", f.Name, getSpaces(columnWidth-len(f.Name)), f.Usage)
				}
			}
		})
	}

	// Parse command-line flags
	config, err := parseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error in configuration: %v\n\n", err)
		flag.Usage()
		os.Exit(1)
	}

	// Print the mode-specific information
	switch config.Mode {
	case "coord_to_vegref":
		if config.CoordToVegref == nil {
			fmt.Fprintf(os.Stderr, "Error: coord_to_vegref configuration is not initialized\n")
			os.Exit(1)
		}

		fmt.Println("Starting conversion of coordinates to vegreferanse using NVDB API v4...")
		fmt.Println("Input file: ", config.InputPath)
		fmt.Println("Output file:", config.OutputPath)
		fmt.Printf("Coordinate columns: X=%d, Y=%d (0-based indices in tab-delimited file)\n",
			config.CoordToVegref.XColumn, config.CoordToVegref.YColumn)

	case "vegref_to_coord":
		if config.VegrefToCoord == nil {
			fmt.Fprintf(os.Stderr, "Error: vegref_to_coord configuration is not initialized\n")
			os.Exit(1)
		}

		fmt.Println("Starting conversion of vegreferanse to coordinates using NVDB API v4...")
		fmt.Println("Input file: ", config.InputPath)
		fmt.Println("Output file:", config.OutputPath)
		fmt.Printf("Vegreferanse column: %d (0-based index in tab-delimited file)\n",
			config.VegrefToCoord.VegreferanseColumn)
	}

	// Set up disk cache
	cacheDirPath := setupCache(config)

	// Create the API client using the v4 implementation
	apiClient := NewVegvesenetAPIV4(
		config.RateLimit,
		time.Duration(config.RateLimitTime)*time.Millisecond,
		cacheDirPath,
	)

	// Print cache statistics if disk cache is enabled
	if apiClient.diskCache != nil {
		count, size, err := apiClient.diskCache.Stats()
		if err != nil {
			fmt.Printf("Failed to get cache statistics: %v\n", err)
		} else {
			fmt.Printf("Using disk cache with %d entries (%.2f MB)\n", count, float64(size)/(1024*1024))
		}
	} else {
		fmt.Println("Disk cache is disabled.")
	}

	fmt.Printf("Processing file %s using %d workers\n", config.InputPath, config.Workers)
	fmt.Printf("Mode: %s\n", config.Mode)
	fmt.Printf("API rate limit: %d calls per %dms (%.1f calls/second)\n",
		config.RateLimit, config.RateLimitTime, float64(config.RateLimit)*1000/float64(config.RateLimitTime))

	startTime := time.Now()
	err = processFile(config.InputPath, config.OutputPath, apiClient, config)
	elapsedTime := time.Since(startTime)

	if err != nil {
		fmt.Printf("Error processing file %s: %v\n", config.InputPath, err)
	} else {
		fmt.Printf("Successfully processed %s -> %s in %v\n", config.InputPath, config.OutputPath, elapsedTime)
	}

	// Print final cache statistics
	if apiClient.diskCache != nil {
		count, size, err := apiClient.diskCache.Stats()
		if err != nil {
			fmt.Printf("Failed to get cache statistics: %v\n", err)
		} else {
			fmt.Printf("Final disk cache: %d entries (%.2f MB)\n", count, float64(size)/(1024*1024))
		}
	}

	fmt.Println("Conversion completed.")
}

// Helper function to check if a string is in a slice
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// Helper function to generate spaces for alignment
func getSpaces(count int) string {
	if count < 1 {
		return "  "
	}
	return strings.Repeat(" ", count)
}
