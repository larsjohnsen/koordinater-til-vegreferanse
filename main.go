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
	"strconv"
	"strings"
	"sync"
	"time"
)

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

// RateLimiter handles API rate limiting
type RateLimiter struct {
	calls     []time.Time
	limit     int
	timeFrame time.Duration
	mu        sync.Mutex
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

// processFile processes a single input file and generates an output file with vegreferanse data
func processFile(inputPath, outputPath string, provider VegreferanseProvider, workers int, xColumn, yColumn int) error {
	// Create directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Open input file
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer inputFile.Close()

	// Create output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	scanner := bufio.NewScanner(inputFile)
	writer := bufio.NewWriter(outputFile)
	defer writer.Flush()

	// Process header
	lineNumber := 0
	var header string
	if scanner.Scan() {
		lineNumber++
		header = scanner.Text()
		// Write header with additional vegreferanse column
		fmt.Fprintf(writer, "%s\tVegreferanse\n", header)
	}

	// Verify columns in header
	headerColumns := strings.Split(header, "\t")
	expectedColumnCount := len(headerColumns)

	// Validate X and Y column indices
	if xColumn < 0 || xColumn >= expectedColumnCount {
		return fmt.Errorf("column X index %d is out of range (file has %d columns)", xColumn, expectedColumnCount)
	}
	if yColumn < 0 || yColumn >= expectedColumnCount {
		return fmt.Errorf("column Y index %d is out of range (file has %d columns)", yColumn, expectedColumnCount)
	}

	fmt.Printf("Input file has %d columns. Using column %d for X and column %d for Y coordinates\n",
		expectedColumnCount, xColumn, yColumn)

	// Read all data lines into memory - complete the import before processing
	var lines []string
	for scanner.Scan() {
		lineNumber++
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input file: %w", err)
	}

	fmt.Printf("Read %d lines from file\n", lineNumber)

	// Create a selector to help choose the best vegreferanse based on travel continuity
	selector := NewVegreferanseSelector(10) // Keep track of last 10 vegreferanses

	// Create a mutex to protect writing to the output file
	var writerMutex sync.Mutex

	// Create a channel for coordinates to process
	type processTask struct {
		lineIdx int
		line    string
	}
	taskCh := make(chan processTask, len(lines))

	// Create a channel for results
	type processResult struct {
		lineIdx      int
		line         string
		vegreferanse string
		matches      []VegreferanseMatch
		err          error
	}
	resultCh := make(chan processResult, len(lines))

	// Create a wait group to wait for all workers to finish
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				line := task.line
				columns := strings.Split(line, "\t")

				// Skip lines with insufficient columns
				if len(columns) < expectedColumnCount {
					resultCh <- processResult{
						lineIdx:      task.lineIdx,
						line:         line,
						vegreferanse: "",
						err:          fmt.Errorf("insufficient columns (found %d, expected %d)", len(columns), expectedColumnCount),
					}
					continue
				}

				// Parse X and Y coordinates
				x, err := strconv.ParseFloat(columns[xColumn], 64)
				if err != nil {
					resultCh <- processResult{
						lineIdx:      task.lineIdx,
						line:         line,
						vegreferanse: "",
						err:          fmt.Errorf("invalid X coordinate at column %d: %w", xColumn, err),
					}
					continue
				}

				y, err := strconv.ParseFloat(columns[yColumn], 64)
				if err != nil {
					resultCh <- processResult{
						lineIdx:      task.lineIdx,
						line:         line,
						vegreferanse: "",
						err:          fmt.Errorf("invalid Y coordinate at column %d: %w", yColumn, err),
					}
					continue
				}

				// Get all vegreferanse matches
				matches, err := provider.GetVegreferanseMatches(x, y)
				if err != nil {
					resultCh <- processResult{
						lineIdx:      task.lineIdx,
						line:         line,
						vegreferanse: "",
						err:          fmt.Errorf("error getting vegreferanse: %w", err),
					}
					continue
				}

				// Get the vegreferanse
				var vegreferanse string
				if len(matches) > 0 {
					// We'll handle selector in the main thread to maintain consistency
					vegreferanse = matches[0].Vegsystemreferanse.Kortform
				}

				resultCh <- processResult{
					lineIdx:      task.lineIdx,
					line:         line,
					vegreferanse: vegreferanse,
					matches:      matches,
					err:          nil,
				}
			}
		}()
	}

	// Queue all tasks
	for i, line := range lines {
		taskCh <- processTask{lineIdx: i, line: line}
	}
	close(taskCh)

	// Create a goroutine to collect results
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Process results
	processedCount := 0
	totalCount := len(lines)
	results := make([]processResult, totalCount)
	noVegreferanseCount := 0
	maxWarningCount := 10 // Only show individual warnings for at most this many coordinates

	// Collect results
	for result := range resultCh {
		results[result.lineIdx] = result
		processedCount++

		// Progress update every 100 lines or 10%
		if processedCount%100 == 0 || processedCount*100/totalCount > (processedCount-1)*100/totalCount {
			fmt.Printf("Processed %d/%d lines (%.1f%%)\n",
				processedCount, totalCount, float64(processedCount)*100/float64(totalCount))
		}
	}

	// Apply vegreferanse selector to all results in order
	// This ensures we maintain road continuity throughout the entire dataset
	var selectorMutex sync.Mutex

	// First pass - apply selector in sequential order
	for i := range results {
		result := &results[i]
		if len(result.matches) > 0 {
			selectorMutex.Lock()
			result.vegreferanse = selector.SelectBestMatch(result.matches)
			selector.AddToHistory(result.vegreferanse)
			selectorMutex.Unlock()
		}
	}

	// Write results in order and ensure consistent column count
	for i, result := range results {
		// Get vegreferanse from earlier selection
		var vegreferanse string = result.vegreferanse

		// If we didn't get a vegreferanse, count it
		if vegreferanse == "" {
			// Count lines without vegreferanse
			noVegreferanseCount++
			// Generate a warning for this specific line, but limit the number of warnings
			if noVegreferanseCount <= maxWarningCount {
				columns := strings.Split(result.line, "\t")
				if len(columns) >= max(xColumn+1, yColumn+1) {
					fmt.Printf("Warning: Coordinate at line %d (%s, %s) did not match any vegreferanse\n",
						i+2, columns[xColumn], columns[yColumn]) // +2 because i is 0-indexed and we have a header
				} else {
					fmt.Printf("Warning: Line %d has insufficient columns and did not match any vegreferanse\n", i+2)
				}
			} else if noVegreferanseCount == maxWarningCount+1 {
				fmt.Printf("Warning: Additional coordinates without vegreferanse exist (suppressing further individual warnings)\n")
			}
		}

		// Ensure consistent column count in output
		columns := strings.Split(result.line, "\t")
		formattedLine := result.line

		// If the line has fewer columns than expected, pad with empty strings
		if len(columns) < expectedColumnCount {
			missingColumns := expectedColumnCount - len(columns)
			formattedLine += strings.Repeat("\t", missingColumns)
		}

		// Write to output file
		writerMutex.Lock()
		fmt.Fprintf(writer, "%s\t%s\n", formattedLine, vegreferanse)
		writerMutex.Unlock()

		// Log errors
		if result.err != nil {
			fmt.Printf("Warning: Line %d: %v\n", i+2, result.err)
		}
	}

	// Verify column count in the output (excluding the header line)
	fmt.Printf("Processing complete. Total lines processed: %d\n", totalCount)

	// Report on vegreferanse matching statistics
	if noVegreferanseCount > 0 {
		fmt.Printf("Warning: %d coordinates (%.1f%%) did not match any vegreferanse\n",
			noVegreferanseCount,
			float64(noVegreferanseCount)*100/float64(totalCount))
	}

	return nil
}

// Helper function to get maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	// Define command-line flags
	var (
		disableCache  bool
		cacheDir      string
		clearCache    bool
		searchRadius  int
		rateLimit     int
		rateLimitTime int
		workers       int
		inputPath     string
		outputPath    string
		xColumn       int
		yColumn       int
	)

	flag.BoolVar(&disableCache, "no-cache", false, "Disable disk cache")
	flag.StringVar(&cacheDir, "cache-dir", "cache/api_responses", "Directory for disk cache")
	flag.BoolVar(&clearCache, "clear-cache", false, "Clear existing cache before starting")
	flag.IntVar(&searchRadius, "radius", 10, "Search radius in meters")
	flag.IntVar(&rateLimit, "rate-limit", 40, "Number of API calls allowed per time frame (NVDB default: 40)")
	flag.IntVar(&rateLimitTime, "rate-time", 1000, "Rate limit time frame in milliseconds (NVDB default: 1000)")
	flag.IntVar(&workers, "workers", 5, "Number of concurrent workers")
	flag.StringVar(&inputPath, "input", "input/7834.txt", "Input file path")
	flag.StringVar(&outputPath, "output", "output/7834_with_vegreferanse.txt", "Output file path")
	flag.IntVar(&xColumn, "x-column", 4, "0-based index of the column containing X coordinates (tab-delimited)")
	flag.IntVar(&yColumn, "y-column", 5, "0-based index of the column containing Y coordinates (tab-delimited)")

	flag.Parse()

	fmt.Println("Starting conversion of coordinates to vegreferanse using NVDB API v4...")
	fmt.Println("Input file: ", inputPath)
	fmt.Println("Output file:", outputPath)
	fmt.Printf("Coordinate columns: X=%d, Y=%d (0-based indices in tab-delimited file)\n", xColumn, yColumn)

	// Set up disk cache
	cacheDirPath := ""
	if !disableCache {
		cacheDirPath = cacheDir
		if err := os.MkdirAll(cacheDirPath, 0755); err != nil {
			fmt.Printf("Warning: Failed to create cache directory: %v\n", err)
			cacheDirPath = "" // Disable disk cache if we can't create the directory
		} else if clearCache {
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
	}

	// Create the API client using the v4 implementation
	apiClient := NewVegvesenetAPIV4(
		rateLimit, // Rate limit (calls per time frame)
		time.Duration(rateLimitTime)*time.Millisecond, // Time frame
		searchRadius, // Search radius in meters
		cacheDirPath, // Disk cache path
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

	fmt.Printf("Processing file %s using %d workers\n", inputPath, workers)
	fmt.Printf("Using column %d for X coordinates and column %d for Y coordinates\n", xColumn, yColumn)
	fmt.Printf("API rate limit: %d calls per %dms (%.1f calls/second)\n",
		rateLimit, rateLimitTime, float64(rateLimit)*1000/float64(rateLimitTime))

	startTime := time.Now()
	err := processFile(inputPath, outputPath, apiClient, workers, xColumn, yColumn)
	elapsedTime := time.Since(startTime)

	if err != nil {
		fmt.Printf("Error processing file %s: %v\n", inputPath, err)
	} else {
		fmt.Printf("Successfully processed %s -> %s in %v\n", inputPath, outputPath, elapsedTime)
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
