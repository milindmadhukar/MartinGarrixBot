package utils

import (
	"strings"
	"unicode"
)

// SimilarityScore returns a normalized similarity score between 0 and 1
// where 1 means exact match and 0 means completely different
func SimilarityScore(s1, s2 string) float64 {
	// Preprocess strings
	s1 = preprocessString(s1)
	s2 = preprocessString(s2)

	// If strings are equal after preprocessing, return 1
	if s1 == s2 {
		return 1.0
	}

	// Calculate Levenshtein distance
	distance := levenshteinDistance(s1, s2)

	// Normalize the score
	maxLen := float64(max(len(s1), len(s2)))
	if maxLen == 0 {
		return 1.0
	}

	return 1.0 - float64(distance)/maxLen
}

// IsCloseMatch checks if two strings match within a given threshold
func IsCloseMatch(target, input string, threshold float64) bool {
	return SimilarityScore(target, input) >= threshold
}

// preprocessString normalizes the string for comparison
func preprocessString(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Remove all spaces
	// TEST: Idk if this works
	s = strings.ReplaceAll(s, " ", "")

	// Remove non-alphanumeric characters except spaces
	var result strings.Builder
	for _, ch := range s {
		if unicode.IsLetter(ch) || unicode.IsNumber(ch) || ch == ' ' {
			result.WriteRune(ch)
		}
	}

	return result.String()
}

// levenshteinDistance calculates the minimum number of single-character edits
func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	// Create matrix of size (len(s1)+1) x (len(s2)+1)
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}

	// Initialize first row and column
	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}

	// Fill rest of the matrix
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			if s1[i-1] == s2[j-1] {
				matrix[i][j] = matrix[i-1][j-1]
			} else {
				matrix[i][j] = min(
					matrix[i-1][j]+1,   // deletion
					matrix[i][j-1]+1,   // insertion
					matrix[i-1][j-1]+1, // substitution
				)
			}
		}
	}

	return matrix[len(s1)][len(s2)]
}

func min(nums ...int) int {
	result := nums[0]
	for _, num := range nums[1:] {
		if num < result {
			result = num
		}
	}
	return result
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}