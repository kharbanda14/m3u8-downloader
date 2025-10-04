package validator

import (
	"fmt"
	"io"
	"os"
)

// ValidateTS checks if a TS file appears to be valid
func ValidateTS(fileName string) error {
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
