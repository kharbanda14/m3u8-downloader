package downloader

import (
	"bufio"
	"fmt"
	"strings"

	"m3u8-downloader/pkg/utils"
)

// ParseBandwidth extracts bandwidth value from stream info
func parseBandwidth(streamInfo string) int {
	bandwidth := 0
	if idx := strings.Index(streamInfo, "BANDWIDTH="); idx != -1 {
		part := streamInfo[idx+10:]
		fmt.Sscanf(part, "%d", &bandwidth)
	}
	return bandwidth
}

// IsMasterPlaylist checks if the content is a master playlist
func IsMasterPlaylist(content string) bool {
	return strings.Contains(content, "#EXT-X-STREAM-INF")
}

// SelectVariantStream selects the highest bandwidth stream from a master playlist
func SelectVariantStream(content, baseURL string) (string, error) {
	var highestBandwidth int
	var selectedURL string

	scanner := bufio.NewScanner(strings.NewReader(content))
	var streamInfo string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			streamInfo = line
		} else if streamInfo != "" && !strings.HasPrefix(line, "#") {
			bandwidth := parseBandwidth(streamInfo)
			if bandwidth > highestBandwidth {
				highestBandwidth = bandwidth
				selectedURL = line
			}
			streamInfo = ""
		}
	}

	if selectedURL == "" {
		return "", fmt.Errorf("no valid streams found in master playlist")
	}

	// Check if the URL is relative
	if !strings.HasPrefix(selectedURL, "http") {
		selectedURL = utils.ResolveURL(baseURL, selectedURL)
	}

	fmt.Printf("Selected stream with bandwidth: %d\n", highestBandwidth)
	return selectedURL, nil
}

// ParseSegments extracts segment URLs from playlist content
func ParseSegments(content, baseURL string) ([]string, error) {
	var segments []string
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip empty lines and comments/tags
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle relative URLs
		if !strings.HasPrefix(line, "http") {
			line = utils.ResolveURL(baseURL, line)
		}

		segments = append(segments, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return segments, nil
}
