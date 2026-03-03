package research

import "strings"

// mergeSources deduplicates sources by URL, keeping the first occurrence.
func mergeSources(existing, newSources []Source) []Source {
	seen := make(map[string]bool, len(existing))
	for _, s := range existing {
		seen[s.URL] = true
	}

	merged := make([]Source, len(existing))
	copy(merged, existing)

	for _, s := range newSources {
		if !seen[s.URL] {
			seen[s.URL] = true
			merged = append(merged, s)
		}
	}
	return merged
}

// mergeClaims deduplicates claims by checking if the statement is substantially similar.
// Uses simple string containment — good enough for dedup within a research session.
func mergeClaims(existing, newClaims []Claim) []Claim {
	merged := make([]Claim, len(existing))
	copy(merged, existing)

	for _, nc := range newClaims {
		isDup := false
		ncLower := strings.ToLower(nc.Statement)
		for _, ec := range existing {
			ecLower := strings.ToLower(ec.Statement)
			// If one contains the other (or they're very similar), it's a duplicate.
			if strings.Contains(ncLower, ecLower) || strings.Contains(ecLower, ncLower) {
				isDup = true
				break
			}
		}
		if !isDup {
			merged = append(merged, nc)
		}
	}
	return merged
}
