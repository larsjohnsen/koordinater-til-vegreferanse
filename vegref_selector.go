// Vegreferanse Selector Component
//
// This component selects the most appropriate road reference (vegreferanse) when multiple matches
// are found based on the following criteria:
// - Continuity with previous road segments (maintaining travel continuity)
// - Prioritizing matches on the same road (same category and number)
// - Considering the physical distance from the coordinate point
//
// The algorithm assigns scores to potential matches and selects the option that best maintains
// the continuity of travel, even if it's not the physically closest match to the coordinate.

package main

import (
	"fmt"
	"strings"
)

// VegreferanseSelector helps select the most appropriate vegreferanse from multiple matches
// based on continuity of travel
type VegreferanseSelector struct {
	// History of recent vegreferanse selections (from oldest to newest)
	history []string
	// Maximum number of history items to maintain
	maxHistory int
}

// NewVegreferanseSelector creates a new selector with the specified history size
func NewVegreferanseSelector(maxHistory int) *VegreferanseSelector {
	return &VegreferanseSelector{
		history:    make([]string, 0, maxHistory),
		maxHistory: maxHistory,
	}
}

// AddToHistory adds a vegreferanse to the history
func (s *VegreferanseSelector) AddToHistory(vegreferanse string) {
	if vegreferanse == "" {
		return // Don't add empty references
	}

	// Add to history
	s.history = append(s.history, vegreferanse)

	// Trim history if too long
	if len(s.history) > s.maxHistory {
		s.history = s.history[1:]
	}
}

// SelectBestMatch selects the best vegreferanse match from the available options
// based on continuity with previous travels
func (s *VegreferanseSelector) SelectBestMatch(matches []VegreferanseMatch) string {
	if len(matches) == 0 {
		return ""
	}

	// If only one match or no history, return the first/closest match
	if len(matches) == 1 || len(s.history) == 0 {
		return matches[0].Vegsystemreferanse.Kortform
	}

	// Get the most recent vegreferanse for comparison
	lastVegreferanse := s.history[len(s.history)-1]

	// First, try to find a perfect road category and number match
	bestMatch := -1
	bestScore := -1
	closestMatchDistance := matches[0].Avstand
	closestMatchIndex := 0

	for i, match := range matches {
		currentVegreferanse := match.Vegsystemreferanse.Kortform
		score := s.calculateMatchScore(lastVegreferanse, currentVegreferanse, match.Avstand)

		if score > bestScore {
			bestScore = score
			bestMatch = i
		}

		// Keep track of the actual closest match by distance
		if match.Avstand < closestMatchDistance {
			closestMatchDistance = match.Avstand
			closestMatchIndex = i
		}
	}

	if bestMatch >= 0 {
		// Only log if the selected match is significantly further away than the closest one
		// Define a threshold for what's considered "significantly" different (e.g., 1 meter or 20% further)
		const distanceThreshold = 1.0   // 1 meter
		const percentageThreshold = 0.2 // 20%

		selectedDistance := matches[bestMatch].Avstand
		selectedVegreferanse := matches[bestMatch].Vegsystemreferanse.Kortform
		closestVegreferanse := matches[closestMatchIndex].Vegsystemreferanse.Kortform

		// Only log if the selected match is not the closest one AND the difference is significant
		if bestMatch != closestMatchIndex &&
			(selectedDistance > closestMatchDistance+distanceThreshold ||
				selectedDistance > closestMatchDistance*(1.0+percentageThreshold)) {

			// Extract road information for logging
			prevParts := strings.Fields(lastVegreferanse)
			selParts := strings.Fields(selectedVegreferanse)
			closeParts := strings.Fields(closestVegreferanse)

			prevRoad := ""
			selRoad := ""
			closeRoad := ""

			if len(prevParts) > 0 {
				prevRoad = prevParts[0]
			}
			if len(selParts) > 0 {
				selRoad = selParts[0]
			}
			if len(closeParts) > 0 {
				closeRoad = closeParts[0]
			}

			fmt.Printf("Road Continuity: Selected %s (%.2fm away) over closest %s (%.2fm away) because it better matches previous road %s\n",
				selectedVegreferanse, selectedDistance, closestVegreferanse, closestMatchDistance, lastVegreferanse)

			// More detailed reason
			if selRoad == prevRoad && closeRoad != prevRoad {
				fmt.Printf("  - Reason: Selected road ID '%s' exactly matches previous road ID '%s'\n", selRoad, prevRoad)
			} else {
				selCategory := extractCategory(selRoad)
				prevCategory := extractCategory(prevRoad)
				closeCategory := extractCategory(closeRoad)

				if selCategory == prevCategory && closeCategory != prevCategory {
					fmt.Printf("  - Reason: Selected road category '%s' matches previous road category '%s'\n", selCategory, prevCategory)
				}

				// Check for section match
				if len(prevParts) > 1 && len(selParts) > 1 && len(closeParts) > 1 {
					prevSection := prevParts[1]
					selSection := selParts[1]
					closeSection := closeParts[1]

					if selSection == prevSection && closeSection != prevSection {
						fmt.Printf("  - Reason: Selected section '%s' matches previous section '%s'\n", selSection, prevSection)
					}
				}
			}
		}
		return matches[bestMatch].Vegsystemreferanse.Kortform
	}

	// Fallback to the closest match if no good continuity match was found
	return matches[0].Vegsystemreferanse.Kortform
}

// calculateMatchScore assigns a score to a potential match based on:
// 1. Continuity with previous road (same category, number, section)
// 2. Physical distance from the coordinate point
func (s *VegreferanseSelector) calculateMatchScore(previous, current string, distance float64) int {
	// Higher score is better
	score := 0

	// Prioritize continuity - parse the vegreferanse strings
	// Format examples: "E5 S1D1 m1000", "Kv12345 S1D1 m100"
	prevParts := strings.Fields(previous)
	currParts := strings.Fields(current)

	if len(prevParts) < 1 || len(currParts) < 1 {
		return 0
	}

	// Extract road identifier (e.g., "E5", "Kv12345")
	prevRoad := prevParts[0]
	currRoad := currParts[0]

	// Major bonus for same road
	if prevRoad == currRoad {
		score += 1000
	} else {
		// Check if same road category (e.g., "E", "Kv")
		prevCategory := extractCategory(prevRoad)
		currCategory := extractCategory(currRoad)

		if prevCategory == currCategory {
			score += 100
		}
	}

	// Check for same section if available
	if len(prevParts) > 1 && len(currParts) > 1 {
		prevSection := prevParts[1]
		currSection := currParts[1]

		if prevSection == currSection {
			score += 50
		}
	}

	// Adjust score based on physical distance (closer is better)
	// Subtract distance (in meters) from score, so closer matches get higher scores
	distanceAdjustment := int(distance * 10)
	score -= distanceAdjustment

	return score
}

// extractCategory gets the road category from a road identifier
// e.g., "E5" -> "E", "Kv12345" -> "Kv"
func extractCategory(road string) string {
	for i, char := range road {
		if char >= '0' && char <= '9' {
			return road[:i]
		}
	}
	return road
}
