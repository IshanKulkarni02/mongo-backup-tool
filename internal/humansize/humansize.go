// Package humansize formats byte counts for display.
package humansize

import "fmt"

// Format renders a byte count as a human-readable string, e.g. "4.2 MB".
func Format(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := "KMGTPE"
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), units[exp])
}
