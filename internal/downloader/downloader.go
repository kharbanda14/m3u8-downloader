package downloader

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kharbanda14/m3u8-downloader/internal/config"
	"github.com/kharbanda14/m3u8-downloader/internal/validator"
	"github.com/kharbanda14/m3u8-downloader/pkg/utils"
)

const (
	downloadBufferSize = 4 * 1024 * 1024 // 4MB for downloads
	mergeBufferSize    = 1 * 1024 * 1024 // 1MB for merging
)

// Downloader handles M3U8 playlist downloading and processing
type Downloader struct {
	config *config.Config
}

// New creates a new Downloader instance
func New(cfg *config.Config) *Downloader {
	return &Downloader{config: cfg}
}

// Download starts the M3U8 download process
func (d *Downloader) Download() error {
	fmt.Println("Starting M3U8 downloader...")
	fmt.Printf("URL: %s\n", d.config.URL)
	fmt.Printf("Output: %s\n", d.config.Output)
	fmt.Printf("Threads: %d\n", d.config.Threads)
	fmt.Printf("Max retry: %d\n", d.config.MaxRetry)
	fmt.Printf("Validation: %v\n", d.config.ValidateFiles)

	// Parse the base URL
	baseURL, err := utils.GetBaseURL(d.config.URL)
	if err != nil {
		return fmt.Errorf("error parsing base URL: %w", err)
	}

	// Get M3U8 content
	playlistContent, err := utils.FetchURL(d.config.URL, d.config.Timeout)
	if err != nil {
		return fmt.Errorf("error fetching M3U8 playlist: %w", err)
	}

	// Check if this is a master playlist (contains variants)
	if IsMasterPlaylist(playlistContent) {
		fmt.Println("Detected master playlist, selecting a stream...")
		variantURL, err := SelectVariantStream(playlistContent, baseURL)
		if err != nil {
			return fmt.Errorf("error selecting variant stream: %w", err)
		}

		// Update the URL and base URL
		d.config.URL = variantURL
		baseURL, err = utils.GetBaseURL(variantURL)
		if err != nil {
			return fmt.Errorf("error parsing variant base URL: %w", err)
		}

		// Fetch the selected playlist
		playlistContent, err = utils.FetchURL(variantURL, d.config.Timeout)
		if err != nil {
			return fmt.Errorf("error fetching variant playlist: %w", err)
		}
	}

	// Parse segments from playlist
	segments, err := ParseSegments(playlistContent, baseURL)
	if err != nil {
		return fmt.Errorf("error parsing segments: %w", err)
	}

	if len(segments) == 0 {
		return fmt.Errorf("no segments found in playlist")
	}

	fmt.Printf("Found %d segments to download\n", len(segments))

	// Download segments
	segmentFiles, err := d.downloadSegments(segments)
	if err != nil {
		return fmt.Errorf("error downloading segments: %w", err)
	}

	// Merge segments into a single file
	if err := d.mergeSegments(segmentFiles); err != nil {
		return fmt.Errorf("error merging segments: %w", err)
	}

	// Clean up temporary files if successful
	fmt.Println("Cleaning up temporary files...")
	for _, file := range segmentFiles {
		os.Remove(file)
	}
	os.RemoveAll(d.config.OutputDir)

	fmt.Printf("Process completed successfully! File saved as: %s\n", d.config.Output)
	return nil
}

// downloadFile downloads a single file with proper error handling and retries
func (d *Downloader) downloadFile(url, fileName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), d.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", utils.UserAgent)
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept", "*/*")

	transport := &http.Transport{
		DisableCompression:  true,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status code: %d", resp.StatusCode)
	}

	tempFileName := fileName + ".tmp"
	out, err := os.Create(tempFileName)
	if err != nil {
		return err
	}

	bufferedWriter := bufio.NewWriter(out)
	_, err = io.Copy(bufferedWriter, resp.Body)
	if err != nil {
		out.Close()
		os.Remove(tempFileName)
		return err
	}

	if err = bufferedWriter.Flush(); err != nil {
		out.Close()
		os.Remove(tempFileName)
		return err
	}

	out.Close()

	if err = os.Rename(tempFileName, fileName); err != nil {
		os.Remove(tempFileName)
		return err
	}

	return nil
}

// downloadSegments downloads all segments concurrently
func (d *Downloader) downloadSegments(segments []string) ([]string, error) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, d.config.Threads)
	var mu sync.Mutex
	var errorOccurred bool

	segmentFiles := make([]string, len(segments))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var failedSegments []int
	var progress atomic.Uint32
	total := uint32(len(segments))
	progress.Store(0)

	fmt.Printf("Starting download of %d segments with %d concurrent threads\n",
		len(segments), d.config.Threads)

	// First download attempt
	for i, segmentURL := range segments {
		wg.Add(1)
		go func(i int, segmentURL string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			fileName := filepath.Join(d.config.OutputDir, fmt.Sprintf("segment_%05d.ts", i))
			err := d.downloadFile(segmentURL, fileName)

			mu.Lock()
			if err == nil {
				segmentFiles[i] = fileName
				progress.Add(1)
				fmt.Printf("\rProgress: %.2f%%", (float64(progress.Load())/float64(total))*100)
			} else {
				failedSegments = append(failedSegments, i)
				fmt.Printf("\nInitial download failed for segment %d: %v (will retry)\n", i, err)
			}
			mu.Unlock()
		}(i, segmentURL)
	}

	wg.Wait()

	// Retry failed segments
	for retryCount := 1; retryCount <= d.config.MaxRetry; retryCount++ {
		if len(failedSegments) == 0 {
			break
		}

		fmt.Printf("\nRetrying %d failed segments (attempt %d/%d)...\n",
			len(failedSegments), retryCount, d.config.MaxRetry)

		backoffTime := time.Duration(retryCount) * time.Second
		time.Sleep(backoffTime)

		var stillFailedSegments []int
		var retryWg sync.WaitGroup

		for _, i := range failedSegments {
			retryWg.Add(1)
			go func(i int) {
				defer retryWg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				select {
				case <-ctx.Done():
					return
				default:
				}

				fileName := filepath.Join(d.config.OutputDir, fmt.Sprintf("segment_%05d.ts", i))
				err := d.downloadFile(segments[i], fileName)

				mu.Lock()
				if err == nil {
					segmentFiles[i] = fileName
					fmt.Printf("Successfully downloaded segment %d on retry %d\n", i+1, retryCount)
				} else {
					stillFailedSegments = append(stillFailedSegments, i)
					if retryCount == d.config.MaxRetry && !errorOccurred {
						errorOccurred = true
						fmt.Printf("Failed to download segment %d after %d retries: %v\n",
							i+1, d.config.MaxRetry, err)
					}
				}
				mu.Unlock()
			}(i)
		}

		retryWg.Wait()
		failedSegments = stillFailedSegments
	}

	// Verify all segments
	for i, file := range segmentFiles {
		if file == "" {
			return nil, fmt.Errorf("segment %d failed to download after all retries", i+1)
		}

		fileInfo, err := os.Stat(file)
		if err != nil {
			return nil, fmt.Errorf("cannot access segment file %d: %w", i+1, err)
		}

		if fileInfo.Size() == 0 {
			return nil, fmt.Errorf("segment file %d is empty (0 bytes)", i+1)
		}

		if d.config.ValidateFiles {
			if err := validator.ValidateTS(file); err != nil {
				return nil, fmt.Errorf("segment file %d failed integrity check: %w", i+1, err)
			}
		}
	}

	fmt.Println("\nAll segments downloaded successfully!")
	return segmentFiles, nil
}

// mergeSegments combines all downloaded segments into a single output file
func (d *Downloader) mergeSegments(segmentFiles []string) error {
	tempOutputFile := d.config.Output + ".tmp"
	out, err := os.Create(tempOutputFile)
	if err != nil {
		return err
	}

	bufferedWriter := bufio.NewWriterSize(out, downloadBufferSize)
	fmt.Println("Merging segments into", d.config.Output)

	buffer := make([]byte, mergeBufferSize)

	for i, file := range segmentFiles {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			out.Close()
			os.Remove(tempOutputFile)
			return fmt.Errorf("segment file missing: %s", file)
		}

		in, err := os.Open(file)
		if err != nil {
			out.Close()
			os.Remove(tempOutputFile)
			return err
		}

		reader := bufio.NewReader(in)

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

		if err := bufferedWriter.Flush(); err != nil {
			out.Close()
			os.Remove(tempOutputFile)
			return err
		}

		if i%10 == 0 || i == len(segmentFiles)-1 {
			fmt.Printf("Merged %d/%d segments\r", i+1, len(segmentFiles))
		}
	}

	if err := bufferedWriter.Flush(); err != nil {
		out.Close()
		os.Remove(tempOutputFile)
		return err
	}

	out.Close()

	if _, err := os.Stat(d.config.Output); err == nil {
		if err := os.Remove(d.config.Output); err != nil {
			os.Remove(tempOutputFile)
			return fmt.Errorf("failed to remove existing output file: %w", err)
		}
	}

	if err := os.Rename(tempOutputFile, d.config.Output); err != nil {
		os.Remove(tempOutputFile)
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	fmt.Println("\nMerge completed successfully!")
	return nil
}
