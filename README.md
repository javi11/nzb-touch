# NZB Touch

<a href="https://www.buymeacoffee.com/qbt52hh7sjd"><img src="https://img.buymeacoffee.com/button-api/?text=Buy me a coffee&emoji=☕&slug=qbt52hh7sjd&button_colour=FFDD00&font_colour=000000&font_family=Comic&outline_colour=000000&coffee_colour=ffffff" /></a>

NZB Touch is a tool for downloading NZB articles from Usenet servers to verify that the file is OK. It can be used to test download speeds, verify article availability, or validate NZB files without storing the downloaded content since it will be store in RAM.

## Commands

### Process a single NZB file

```
nzbtouch -n /path/to/file.nzb -c /path/to/config.yaml
```

Required flags:

- `-n, --nzb` - Path to the NZB file
- `-c, --config` - Path to the YAML configuration file

### Directory scanning mode

```
nzbtouch scan -c /path/to/config.yaml
```

This command will continuously scan configured directories for NZB files and process them according to the settings.

Required flags:

- `-c, --config` - Path to the YAML configuration file

## Configuration

Create a YAML configuration file with your Usenet provider details and other settings. See `config.sample.yaml` for an example configuration:

```yaml
# Download worker settings
download_workers: 20 # Number of concurrent download workers

# Usenet providers configuration
download_providers:
  - host: "news.example.com"
    port: 563
    username: "your_username"
    password: "your_password"
    ssl: true
    max_connections: 10

# Scanner configuration for directory watching
scanner:
  enabled: true # Enable directory scanning
  watch_directories: # List of directories to scan for NZB files
    - "/path/to/nzb/downloads"
  scan_interval: "5m" # Scan interval (5 minutes)
  max_files_per_day: 100 # Maximum number of files to process per day
  concurrent_jobs: 3 # Number of concurrent processing jobs
  database_path: "queue.db" # SQLite database for persistent queue storage
  reprocess_interval: "7d" # Reprocess items after 7 days (set to "0" to disable)
  check_percent: 100 # Percentage how many articles should be downloaded
```

### Scanner Configuration

- `enabled` - Enable or disable the scanner
- `watch_directories` - List of directories to scan for NZB files
- `scan_interval` - How often to scan directories (e.g., "5m", "1h", "30s"). Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
- `max_files_per_day` - Maximum number of files to process per day
- `concurrent_jobs` - Number of concurrent processing jobs
- `database_path` - Path to SQLite database file for persistent queue storage (default: "queue.db")
- `reprocess_interval` - Duration after which to reprocess previously processed files (default: "0" = disabled). Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".

## Building

```
go build -o nzbtouch ./cmd/nzbtouch
```

## Features

- Download the articles in memory and discard
- Scan mode to scan a directory
- Schedule scan with max number of files to download peer day
- Move the broken files into another directory

## Exit Codes

The application uses different exit codes to indicate specific error conditions:

- `0` - Success
- `1` - Missing required arguments (NZB file or config file)
- `2` - Failed to load configuration file
- `3` - Failed to load or parse NZB file
- `4` - Failed to create NNTP connection pool
- `5` - Error processing NZB (download errors, missing segments, etc.)

## Installation

### Prerequisites

- Go 1.24 or higher
- A valid Usenet account with a service provider

### Setup

1. Clone this repository:

```bash
git clone https://github.com/javi11/nzb-touch.git
cd nzb-touch
```

2. Install dependencies:

```bash
go mod tidy
```

3. Build the application:

```bash
go build -o nzbtouch ./cmd/nzbtouch
```

## Usage

Run the application with the required parameters:

```bash
./nzbtouch --nzb path/to/file.nzb --config path/to/config.yml
```

### Command Line Options

```
Usage:
  nzbtouch [flags]

Flags:
  -c, --config string     Path to YAML config file (required)
  -h, --help              help for nzbtouch
  -n, --nzb string        Path to NZB file (required)
  -r, --progress          Show progress during download (default true)
```

## Performance Considerations

### RAM vs Disk

The tool downloads articles to RAM by default (using `io.Discard`), which is more performant but uses no storage. This approach is faster because:

1. No disk I/O bottlenecks
2. No time spent on writing data to disk
3. No storage space required

### Connection Count

Connection settings can be configured in the YAML file. Consider:

- Your Usenet provider's limits
- Your network bandwidth
- Your system's capabilities

Increasing connections can improve download speed but might hit provider limits or saturate your network connection.

## Development

### Adding New Commands

The application uses [Cobra](https://github.com/spf13/cobra) for command-line functionality. To add new commands:

```bash
cd cmd/nzbtouch
```

Then add a new command file and implement the command logic.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
