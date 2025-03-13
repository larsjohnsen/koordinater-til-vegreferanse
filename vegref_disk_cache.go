// Disk Cache Component
//
// This component provides persistent caching of API responses to improve performance
// and reduce the number of API calls needed.
//
// Key features:
// - File-based caching of vegreferanse data indexed by coordinates
// - Thread-safe implementation with proper locking
// - Organizes cache files in subdirectories to prevent too many files in a single directory
// - Provides methods to get, set, clear cache entries and retrieve cache statistics
// - Helps stay within API rate limits by reducing the need for repeated API calls

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// VegreferanseDiskCache implements a persistent cache for API responses
type VegreferanseDiskCache struct {
	cacheDir string
	mu       sync.RWMutex
}

// NewVegreferanseDiskCache creates a new disk cache at the specified directory
func NewVegreferanseDiskCache(cacheDir string) (*VegreferanseDiskCache, error) {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &VegreferanseDiskCache{
		cacheDir: cacheDir,
	}, nil
}

// getCacheFilePath creates a cache file path from coordinates
func (c *VegreferanseDiskCache) getCacheFilePath(x, y float64) string {
	// Format coordinates to 6 decimal places
	key := fmt.Sprintf("%.6f,%.6f", x, y)

	// Replace any characters that might be invalid in filenames
	safeKey := strings.ReplaceAll(key, ",", "_")

	// Group files in subdirectories based on first 4 digits of X coordinate
	// This prevents having too many files in a single directory
	prefix := safeKey[:4]

	// Create subdirectory if it doesn't exist
	subDir := filepath.Join(c.cacheDir, prefix)
	if _, err := os.Stat(subDir); os.IsNotExist(err) {
		_ = os.MkdirAll(subDir, 0755)
	}

	return filepath.Join(subDir, safeKey+".json")
}

// Get retrieves the cached VegreferanseMatches for the given coordinates
// Returns nil and false if no cache entry exists
func (c *VegreferanseDiskCache) Get(x, y float64) ([]VegreferanseMatch, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	filePath := c.getCacheFilePath(x, y)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, false
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Warning: failed to read cache file %s: %v\n", filePath, err)
		return nil, false
	}

	// Parse JSON
	var matches []VegreferanseMatch
	if err := json.Unmarshal(data, &matches); err != nil {
		fmt.Printf("Warning: failed to parse cache file %s: %v\n", filePath, err)
		return nil, false
	}

	return matches, true
}

// Set saves VegreferanseMatches to cache
func (c *VegreferanseDiskCache) Set(x, y float64, matches []VegreferanseMatch) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	filePath := c.getCacheFilePath(x, y)

	// Convert matches to JSON
	data, err := json.Marshal(matches)
	if err != nil {
		return fmt.Errorf("failed to serialize matches: %w", err)
	}

	// Create directories if needed
	dirPath := filepath.Dir(filePath)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create cache subdirectory: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// Clear removes all cached entries
func (c *VegreferanseDiskCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return os.RemoveAll(c.cacheDir)
}

// Stats returns cache statistics
func (c *VegreferanseDiskCache) Stats() (int, int64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var count int
	var totalSize int64

	err := filepath.Walk(c.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".json") {
			count++
			totalSize += info.Size()
		}
		return nil
	})

	return count, totalSize, err
}
