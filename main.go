package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	// Buffer sizes
	downloadBufferSize = 4 * 1024 * 1024 // 4MB for downloads
	mergeBufferSize    = 1 * 1024 * 1024 // 1MB for merging
)

// Config holds the downloader configuration
type Config struct {
	URL           string
	OutputDir     string
	Output        string
	MaxRetry      int
	Threads       int
	Timeout       time.Duration
	ValidateFiles bool // Option to validate integrity of downloaded segments
}

// validateTSFile checks if a TS file appears to be valid
func validateTSFile(fileName string) error {
	// Open the file
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	// Read the first few bytes to check for TS sync byte pattern
	header := make([]byte, 188*3) // Read 3 TS packets
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return err
	}

	// TS packets should start with 0x47 (decimal 71) and be 188 bytes each
	if n < 188 {
		return fmt.Errorf("file too small to be a valid TS segment")
	}

	// Check for sync byte (0x47) at the start of each TS packet
	validSyncBytes := 0
	for i := 0; i < n; i += 188 {
		if i+188 <= n && header[i] == 0x47 {
			validSyncBytes++
		}
	}

	// Ensure we found at least one valid sync pattern
	if validSyncBytes == 0 {
		return fmt.Errorf("no valid TS sync patterns found")
	}

	return nil
}

func main() {
	// Parse command line arguments
	m3u8URL := flag.String("url", "", "M3U8 playlist URL (required)")
	outputDir := flag.String("dir", "downloads", "Directory for temporary files")
	output := flag.String("output", "output.ts", "Output file name")
	maxRetry := flag.Int("retry", 5, "Max retry times when download fails")
	threads := flag.Int("threads", 10, "Number of concurrent downloads")
	timeout := flag.Int("timeout", 30, "Timeout in seconds for HTTP requests")
	validate := flag.Bool("validate", true, "Validate integrity of downloaded segments")
	flag.Parse()

	if *m3u8URL == "" {
		fmt.Println("Error: M3U8 URL is required")
		flag.Usage()
		os.Exit(1)
	}

	// Create configuration
	config := &Config{
		URL:           *m3u8URL,
		OutputDir:     *outputDir,
		Output:        *output,
		MaxRetry:      *maxRetry,
		Threads:       *threads,
		Timeout:       time.Duration(*timeout) * time.Second,
		ValidateFiles: *validate,
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Start downloading
	if err := downloadM3U8(config); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Download completed successfully!")
}

func downloadM3U8(config *Config) error {
	fmt.Println("Starting M3U8 downloader...")
	fmt.Printf("URL: %s\n", config.URL)
	fmt.Printf("Output: %s\n", config.Output)
	fmt.Printf("Threads: %d\n", config.Threads)
	fmt.Printf("Max retry: %d\n", config.MaxRetry)
	fmt.Printf("Validation: %v\n", config.ValidateFiles)

	// Parse the base URL
	baseURL, err := getBaseURL(config.URL)
	if err != nil {
		return fmt.Errorf("error parsing base URL: %w", err)
	}

	// Get M3U8 content
	playlistContent, err := fetchURL(config.URL, config.Timeout)
	if err != nil {
		return fmt.Errorf("error fetching M3U8 playlist: %w", err)
	}

	// Check if this is a master playlist (contains variants)
	if isMasterPlaylist(playlistContent) {
		fmt.Println("Detected master playlist, selecting a stream...")
		variantURL, err := selectVariantStream(playlistContent, baseURL)
		if err != nil {
			return fmt.Errorf("error selecting variant stream: %w", err)
		}

		// Update the URL and base URL
		config.URL = variantURL
		baseURL, err = getBaseURL(variantURL)
		if err != nil {
			return fmt.Errorf("error parsing variant base URL: %w", err)
		}

		// Fetch the selected playlist
		playlistContent, err = fetchURL(variantURL, config.Timeout)
		if err != nil {
			return fmt.Errorf("error fetching variant playlist: %w", err)
		}
	}

	// Parse segments from playlist
	segments, err := parseSegments(playlistContent, baseURL)
	if err != nil {
		return fmt.Errorf("error parsing segments: %w", err)
	}

	if len(segments) == 0 {
		return fmt.Errorf("no segments found in playlist")
	}

	fmt.Printf("Found %d segments to download\n", len(segments))

	// Download segments
	segmentFiles, err := downloadSegments(segments, config)
	if err != nil {
		return fmt.Errorf("error downloading segments: %w", err)
	}

	// Merge segments into a single file
	if err := mergeSegments(segmentFiles, config.Output); err != nil {
		return fmt.Errorf("error merging segments: %w", err)
	}

	// Verify the final file
	fmt.Println("Verifying the final output file...")
	fileInfo, err := os.Stat(config.Output)
	if err != nil {
		return fmt.Errorf("error accessing output file: %w", err)
	}

	if fileInfo.Size() == 0 {
		return fmt.Errorf("output file is empty (0 bytes)")
	}

	fmt.Printf("Output file size: %.2f MB\n", float64(fileInfo.Size())/(1024*1024))

	// Clean up temporary files if successful
	fmt.Println("Cleaning up temporary files...")
	for _, file := range segmentFiles {
		os.Remove(file)
	}
	os.RemoveAll(config.OutputDir)

	fmt.Printf("Process completed successfully! File saved as: %s\n", config.Output)
	return nil
}

func getBaseURL(urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	// Remove the filename part
	dir, _ := path.Split(parsedURL.Path)
	parsedURL.Path = dir

	return parsedURL.String(), nil
}

func fetchURL(urlStr string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func isMasterPlaylist(content string) bool {
	return strings.Contains(content, "#EXT-X-STREAM-INF")
}

func selectVariantStream(content, baseURL string) (string, error) {
	var highestBandwidth int
	var selectedURL string

	scanner := bufio.NewScanner(strings.NewReader(content))
	var streamInfo string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			streamInfo = line
		} else if streamInfo != "" && !strings.HasPrefix(line, "#") {
			// Parse bandwidth from the stream info
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
		selectedURL = resolveURL(baseURL, selectedURL)
	}

	fmt.Printf("Selected stream with bandwidth: %d\n", highestBandwidth)
	return selectedURL, nil
}

func parseBandwidth(streamInfo string) int {
	// Extract BANDWIDTH value
	bandwidth := 0
	if idx := strings.Index(streamInfo, "BANDWIDTH="); idx != -1 {
		part := streamInfo[idx+10:]
		fmt.Sscanf(part, "%d", &bandwidth)
	}
	return bandwidth
}

func resolveURL(baseURL, relURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return relURL
	}

	rel, err := url.Parse(relURL)
	if err != nil {
		return relURL
	}

	resolvedURL := base.ResolveReference(rel)
	return resolvedURL.String()
}

func parseSegments(content, baseURL string) ([]string, error) {
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
			line = resolveURL(baseURL, line)
		}

		segments = append(segments, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return segments, nil
}

func downloadSegments(segments []string, config *Config) ([]string, error) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, config.Threads)
	var mu sync.Mutex
	var errorOccurred bool

	segmentFiles := make([]string, len(segments))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// For tracking downloaded segments and retrying failed ones
	var failedSegments []int

	// Print status
	fmt.Printf("Starting download of %d segments with %d concurrent threads\n",
		len(segments), config.Threads)

	var progress atomic.Uint32
	total := uint32(len(segments))
	progress.Store(0)

	// First download attempt for all segments
	for i, segmentURL := range segments {
		wg.Add(1)
		go func(i int, segmentURL string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			// Check if we should continue
			select {
			case <-ctx.Done():
				return
			default:
				// Continue
			}

			fileName := filepath.Join(config.OutputDir, fmt.Sprintf("segment_%05d.ts", i))
			err := downloadFile(segmentURL, fileName, config.Timeout)

			mu.Lock()
			if err == nil {
				segmentFiles[i] = fileName
				progress.Add(1)

				fmt.Printf("\rProgress: %.2f%%", (float64(progress.Load())/float64(total))*100)

				if progress.Load() == total {
					fmt.Println("\nAll segments downloaded successfully!")
				}
			} else {
				failedSegments = append(failedSegments, i)
				fmt.Printf("\nInitial download failed for segment %d: %v (will retry)\n", i, err)
			}
			mu.Unlock()
		}(i, segmentURL)
	}

	wg.Wait()

	// Retry loop for failed segments, with exponential backoff
	for retryCount := 1; retryCount <= config.MaxRetry; retryCount++ {
		if len(failedSegments) == 0 {
			break
		}

		fmt.Printf("\nRetrying %d failed segments (attempt %d/%d)...\n",
			len(failedSegments), retryCount, config.MaxRetry)

		// Wait before retrying with exponential backoff
		backoffTime := time.Duration(retryCount) * time.Second
		time.Sleep(backoffTime)

		var stillFailedSegments []int
		var retryWg sync.WaitGroup

		for _, i := range failedSegments {
			retryWg.Add(1)
			go func(i int) {
				defer retryWg.Done()
				semaphore <- struct{}{}        // Acquire
				defer func() { <-semaphore }() // Release

				// Check if we should continue
				select {
				case <-ctx.Done():
					return
				default:
					// Continue
				}

				segmentURL := segments[i]
				fileName := filepath.Join(config.OutputDir, fmt.Sprintf("segment_%05d.ts", i))

				// Try to download again
				err := downloadFile(segmentURL, fileName, config.Timeout)

				mu.Lock()
				if err == nil {
					segmentFiles[i] = fileName
					fmt.Printf("Successfully downloaded segment %d on retry %d\n", i+1, retryCount)
				} else {
					stillFailedSegments = append(stillFailedSegments, i)
					if retryCount == config.MaxRetry {
						if !errorOccurred {
							errorOccurred = true
							fmt.Printf("Failed to download segment %d after %d retries: %v\n",
								i+1, config.MaxRetry, err)
						}
					}
				}
				mu.Unlock()
			}(i)
		}

		retryWg.Wait()
		failedSegments = stillFailedSegments
	}

	// Verify all segments were downloaded
	for i, file := range segmentFiles {
		if file == "" {
			return nil, fmt.Errorf("segment %d failed to download after all retries", i+1)
		}

		// Additional integrity check: verify file exists and is not empty
		fileInfo, err := os.Stat(file)
		if err != nil {
			return nil, fmt.Errorf("cannot access segment file %d: %w", i+1, err)
		}

		if fileInfo.Size() == 0 {
			return nil, fmt.Errorf("segment file %d is empty (0 bytes)", i+1)
		}

		// Optional: Validate file integrity if enabled
		if config.ValidateFiles {
			if err := validateTSFile(file); err != nil {
				return nil, fmt.Errorf("segment file %d failed integrity check: %w", i+1, err)
			}
		}
	}

	fmt.Println("\nAll segments downloaded successfully!")
	return segmentFiles, nil
}

func downloadFile(url, fileName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", userAgent)
	// Add additional headers that might help with reliable downloads
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept", "*/*")

	// Use a transport with more reliable settings
	transport := &http.Transport{
		DisableCompression:  true,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status code: %d", resp.StatusCode)
	}

	// Create a temporary file first
	tempFileName := fileName + ".tmp"
	out, err := os.Create(tempFileName)
	if err != nil {
		return err
	}

	// Use buffered writer for better performance and reliability
	bufferedWriter := bufio.NewWriter(out)

	// Copy with a buffer for better handling of larger files
	_, err = io.Copy(bufferedWriter, resp.Body)
	if err != nil {
		out.Close()
		os.Remove(tempFileName)
		return err
	}

	// Flush the buffered writer
	if err = bufferedWriter.Flush(); err != nil {
		out.Close()
		os.Remove(tempFileName)
		return err
	}

	// Close the file before renaming
	out.Close()

	// Rename the temporary file to the final name
	if err = os.Rename(tempFileName, fileName); err != nil {
		os.Remove(tempFileName)
		return err
	}

	return nil
}

func mergeSegments(segmentFiles []string, outputFile string) error {
	// Create a temporary output file first
	tempOutputFile := outputFile + ".tmp"
	out, err := os.Create(tempOutputFile)
	if err != nil {
		return err
	}

	// Use a buffered writer for better performance and reliability
	bufferedWriter := bufio.NewWriterSize(out, downloadBufferSize)

	fmt.Println("Merging segments into", outputFile)

	// Buffer for reading files
	buffer := make([]byte, mergeBufferSize)

	for i, file := range segmentFiles {
		// Verify file exists and is readable
		if _, err := os.Stat(file); os.IsNotExist(err) {
			out.Close()
			os.Remove(tempOutputFile)
			return fmt.Errorf("segment file missing: %s", file)
		}

		// Open the file for reading
		in, err := os.Open(file)
		if err != nil {
			out.Close()
			os.Remove(tempOutputFile)
			return err
		}

		// Use a buffered reader
		reader := bufio.NewReader(in)

		// Copy data in chunks using the buffer
		for {
			n, err := reader.Read(buffer)
			if err != nil && err != io.EOF {
				in.Close()
				out.Close()
				os.Remove(tempOutputFile)
				return err
			}

			if n == 0 {
				break
			}

			if _, err := bufferedWriter.Write(buffer[:n]); err != nil {
				in.Close()
				out.Close()
				os.Remove(tempOutputFile)
				return err
			}
		}

		in.Close()

		// Flush the buffer after each file to ensure data is written
		if err := bufferedWriter.Flush(); err != nil {
			out.Close()
			os.Remove(tempOutputFile)
			return err
		}

		if i%10 == 0 || i == len(segmentFiles)-1 {
			fmt.Printf("Merged %d/%d segments\r", i+1, len(segmentFiles))
		}
	}

	// Flush any remaining buffered data and close the file
	if err := bufferedWriter.Flush(); err != nil {
		out.Close()
		os.Remove(tempOutputFile)
		return err
	}

	out.Close()

	// Remove existing output file if it exists
	if _, err := os.Stat(outputFile); err == nil {
		if err := os.Remove(outputFile); err != nil {
			os.Remove(tempOutputFile)
			return fmt.Errorf("failed to remove existing output file: %w", err)
		}
	}

	// Rename the temporary file to the final output file
	if err := os.Rename(tempOutputFile, outputFile); err != nil {
		os.Remove(tempOutputFile)
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	fmt.Println("\nMerge completed successfully!")
	return nil
}
