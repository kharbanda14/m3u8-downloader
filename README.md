# M3U8 Downloader

This is a Go-based M3U8 downloader that downloads and merges video segments from an M3U8 playlist into a single `.ts` file. It supports master playlists, concurrent downloads, retries, and optional segment validation.

## Features

- Downloads video segments from M3U8 playlists.
- Handles master playlists by selecting the highest bandwidth stream.
- Concurrent downloads with configurable thread count.
- Retry mechanism for failed downloads.
- Validates the integrity of downloaded `.ts` segments (optional).
- Merges all segments into a single output file.

## Requirements

- Go 1.16 or later
- Internet connection

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/your-repo/m3u8-downloader.git
   cd m3u8-downloader
   ```

2. Build the project:
   ```bash
   go build -o m3u8-downloader main.go
   ```

## Usage

Run the downloader with the following options:

```bash
./m3u8-downloader -url <M3U8_URL> [options]
```

### Options

| Option         | Description                                      | Default         |
|----------------|--------------------------------------------------|-----------------|
| `-url`         | M3U8 playlist URL (required).                    |                 |
| `-dir`         | Directory for temporary files.                   | `downloads`     |
| `-output`      | Output file name.                                | `output.ts`     |
| `-retry`       | Max retry times for failed downloads.            | `5`             |
| `-threads`     | Number of concurrent downloads.                  | `10`            |
| `-timeout`     | Timeout in seconds for HTTP requests.            | `30`            |
| `-validate`    | Validate integrity of downloaded segments.       | `true`          |

### Example

```bash
./m3u8-downloader -url https://example.com/playlist.m3u8 -output video.ts -threads 5
```

This will download the playlist and save the merged video as `video.ts`.

## How It Works

1. **Master Playlist Detection**: If the provided M3U8 is a master playlist, the program selects the highest bandwidth stream.
2. **Segment Parsing**: Extracts all segment URLs from the playlist.
3. **Concurrent Downloads**: Downloads segments using multiple threads.
4. **Validation**: Optionally validates the integrity of each `.ts` segment.
5. **Merging**: Combines all segments into a single `.ts` file.

## Error Handling

- Failed downloads are retried up to the specified `-retry` count.
- If a segment fails all retries, the program exits with an error.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests.

## Disclaimer

This tool is intended for educational purposes only. Ensure you have the right to download and use the content from the provided M3U8 URL.