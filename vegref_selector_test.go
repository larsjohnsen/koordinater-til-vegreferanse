package main

import (
	"testing"
)

func TestVegreferanseSelector(t *testing.T) {
	// Create a selector
	selector := NewVegreferanseSelector(5)

	// Test with empty history
	t.Run("EmptyHistory", func(t *testing.T) {
		// Create some test matches
		matches := []VegreferanseMatch{
			{
				Vegsystemreferanse: struct {
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
				}{
					Kortform: "E18 S65D1 m12621",
				},
				Avstand: 2.5,
			},
			{
				Vegsystemreferanse: struct {
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
				}{
					Kortform: "Kv1000 S1D1 m500",
				},
				Avstand: 1.0,
			},
		}

		// With empty history, it should select the first match (sorted by distance)
		result := selector.SelectBestMatch(matches)
		expected := "E18 S65D1 m12621" // first match

		if result != expected {
			t.Errorf("Expected %s, got %s", expected, result)
		}
	})

	// Test with history - should prefer match on same road
	t.Run("WithHistory", func(t *testing.T) {
		// Add some history
		selector.AddToHistory("E18 S65D1 m12500")

		// Create some test matches
		matches := []VegreferanseMatch{
			{
				Vegsystemreferanse: struct {
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
				}{
					Kortform: "Kv1000 S1D1 m500",
				},
				Avstand: 1.0, // This is closer but different road
			},
			{
				Vegsystemreferanse: struct {
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
				}{
					Kortform: "E18 S65D1 m12600",
				},
				Avstand: 3.0, // This is further but same road as history
			},
		}

		// Should prefer E18 because it matches the road in history
		result := selector.SelectBestMatch(matches)
		expected := "E18 S65D1 m12600"

		if result != expected {
			t.Errorf("Expected %s, got %s", expected, result)
		}
	})

	// Test same category but different road number
	t.Run("SameCategory", func(t *testing.T) {
		// Reset selector
		selector = NewVegreferanseSelector(5)
		selector.AddToHistory("E6 S28D1 m3200")

		// Create some test matches
		matches := []VegreferanseMatch{
			{
				Vegsystemreferanse: struct {
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
				}{
					Kortform: "E18 S65D1 m12600", // Same category (E) but different road
				},
				Avstand: 2.0,
			},
			{
				Vegsystemreferanse: struct {
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
				}{
					Kortform: "Fv100 S1D1 m500", // Different category altogether
				},
				Avstand: 1.5,
			},
		}

		// Should prefer E18 because it's the same category as E6
		result := selector.SelectBestMatch(matches)
		expected := "E18 S65D1 m12600"

		if result != expected {
			t.Errorf("Expected %s, got %s", expected, result)
		}
	})
}

func TestExtractCategory(t *testing.T) {
	testCases := []struct {
		road     string
		expected string
	}{
		{"E18", "E"},
		{"Kv1000", "Kv"},
		{"Fv100", "Fv"},
		{"Rv4", "Rv"},
		{"", ""},
		{"NoNumbers", "NoNumbers"},
	}

	for _, tc := range testCases {
		t.Run(tc.road, func(t *testing.T) {
			result := extractCategory(tc.road)
			if result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}
