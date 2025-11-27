package handlers

import (
	"sort"
	"strconv"
	"strings"
)

// variantInfo holds information about a stream variant
type variantInfo struct {
	bandwidth int
	lines     []string // All lines for this variant (STREAM-INF + URL)
}

// reorderCatchupVariants reorders HLS master playlist variants by bandwidth (highest first)
func reorderCatchupVariants(playlist []byte) []byte {
	lines := strings.Split(string(playlist), "\n")

	var header []string // Everything before first variant
	var variants []variantInfo
	var footer []string // Everything after last variant

	inVariants := false
	var currentVariant *variantInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is a STREAM-INF line
		if strings.HasPrefix(trimmed, "#EXT-X-STREAM-INF:") {
			inVariants = true

			// Extract bandwidth
			bandwidth := 0
			if idx := strings.Index(trimmed, "BANDWIDTH="); idx != -1 {
				bwStr := trimmed[idx+10:]
				if commaIdx := strings.Index(bwStr, ","); commaIdx != -1 {
					bwStr = bwStr[:commaIdx]
				}
				bandwidth, _ = strconv.Atoi(bwStr)
			}

			currentVariant = &variantInfo{
				bandwidth: bandwidth,
				lines:     []string{line},
			}
		} else if currentVariant != nil && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			// This is the URL line following STREAM-INF
			currentVariant.lines = append(currentVariant.lines, line)
			variants = append(variants, *currentVariant)
			currentVariant = nil
		} else if !inVariants {
			header = append(header, line)
		} else if inVariants && strings.HasPrefix(trimmed, "#EXT-X-I-FRAME") {
			// Start of I-frame section marks end of variants
			footer = lines[i:]
			break
		}
	}

	for i, v := range variants {
		// Add NAME attribute based on resolution for Flowplayer quality selector
		for j, line := range v.lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-STREAM-INF:") {
				// Extract resolution
				resolution := ""
				if idx := strings.Index(line, "RESOLUTION="); idx != -1 {
					resStr := line[idx+11:]
					if commaIdx := strings.Index(resStr, ","); commaIdx != -1 {
						resolution = resStr[:commaIdx]
					} else {
						resolution = resStr
					}
				}

				// Add NAME based on resolution
				name := ""
				if strings.Contains(resolution, "1920x1080") {
					name = "1080p"
				} else if strings.Contains(resolution, "1280x720") {
					name = "720p"
				} else if strings.Contains(resolution, "854x480") {
					name = "480p"
				} else if strings.Contains(resolution, "640x360") {
					name = "360p"
				} else if strings.Contains(resolution, "320x180") {
					name = "180p"
				} else {
					name = resolution
				}

				// Inject NAME attribute if not already present
				if !strings.Contains(line, "NAME=") && name != "" {
					v.lines[j] = strings.TrimRight(line, "\n") + ",NAME=\"" + name + "\"\n"
				}
			}
		}
		variants[i] = v
	}

	// Sort variants by bandwidth (highest first)
	sort.Slice(variants, func(i, j int) bool {
		return variants[i].bandwidth > variants[j].bandwidth
	})

	// Rebuild playlist
	var result []string
	result = append(result, header...)

	for _, v := range variants {
		result = append(result, v.lines...)
	}

	result = append(result, footer...)

	return []byte(strings.Join(result, "\n"))
}
