# Go Motion Photo

A command-line tool and Go library for handling **Samsung Motion Photos**. Extract video and image components from motion photo files (`.jpg`, `.jpeg`, `.heic`).

## Features

- Process Samsung Motion Photos (both legacy and current formats)
- Process several files, whole directories, glob or regular expression patterns
- Configurable output options
- Supports JPG and HEIC motion photo formats
- Configurable logging system
- Usable as a dependency-free Go library

## Options

### Input Options
- `<path>...` / `--input <path>`: Motion photo files, directories or patterns
    - Single file: `photo.jpg`
    - Several files: `a.jpg b.jpg c.heic`
    - Directory: `./photos` (searched recursively)
    - Regex pattern: `/IMG_\d{4}\.jpg/` (matched against file names in the current directory)
    - Glob pattern: `'*.jpg'`
    - Supported formats: `.jpg`, `.jpeg`, `.heic`

When more than one file is processed, files that are not motion photos are skipped. A single file named directly must be a motion photo.

### Output Options
- `--output <dir>`: Output directory for extracted files (default: current directory)
- `--delete-orig`: Remove original file after successful extraction
- `--rename-orig`: Use base name for extracted files, add `_original` to source file
- `--extract-photo`: Extract photo component (default: true)
- `--extract-video`: Extract video component (default: true)
- `--force`: Overwrite existing output files

### Logging Options
- `--log-file <path>`: Log file path
- `--log-level <level>`: Log level (`debug`, `info`, `warn`, `error`)
- `--no-console-log`: Disable console output

Logs are written to standard error.

### Other
- `--version`: Print version and exit
- `--help`: Show usage

### Exit status
- `0`: every file was extracted or skipped
- `1`: invalid usage, or at least one file failed

## Configuration

Configuration can be provided through, in order of precedence:
- Command line arguments
- Environment variables: `GO_MOTION_PHOTO_` followed by the config key, e.g. `GO_MOTION_PHOTO_OUTPUT`, `GO_MOTION_PHOTO_LOG_LEVEL`
- Configuration file (`go-motion-photo.yaml`)

Default config locations:
- Current directory
- `$HOME/.config/go-motion-photo`

## Usage Examples

Using command-line flags:
```bash
# Process single file
go-motion-photo photo.jpg

# Process several files
go-motion-photo a.jpg b.jpg c.heic

# Specify output location
go-motion-photo --input photo.jpg --output ./extracted

# Process all supported files in directory
go-motion-photo --input ./photos

# Process files matching regex pattern
go-motion-photo --input /IMG_\d{4}\.jpg/

# Keep original naming scheme
go-motion-photo --input photo.jpg --rename-orig

# Extract only the photo component
go-motion-photo --input photo.jpg --extract-video=false

# Process HEIC file and overwrite existing outputs
go-motion-photo --input photo.heic --force
```

Using configuration file (`go-motion-photo.yaml`):
```yaml
input: "./photos"
output: "./extracted"
delete_orig: false
rename_orig: true
extract_video: false
log:
  file: "motion-photo.log"
  level: "info"
  no_console: false
```

## Installation

Binary releases are available for Linux, macOS, and Windows on the [releases page](https://github.com/ikerls/motion-photo-extractor/releases).

With Go installed:
```bash
go install github.com/ikerls/motion-photo-extractor/cmd/go-motion-photo@latest
```

## Library

```bash
go get github.com/ikerls/motion-photo-extractor
```

Extract a file on disk:
```go
import "github.com/ikerls/motion-photo-extractor/pkg/extractor"

res, err := extractor.ExtractFile("photo.jpg", extractor.Options{OutputDir: "./extracted"})
if errors.Is(err, extractor.ErrNotMotionPhoto) {
	// a regular photo
}
fmt.Println(res.PhotoPath, res.VideoPath)
```

Or work on bytes, without touching the filesystem:
```go
parts, err := extractor.Split(data)
if err != nil {
	return err
}
extractor.SanitizePhoto(parts.Photo) // stop the photo from advertising a video
// parts.Photo and parts.Video are slices of data
```

Note: This tool is specifically designed for Samsung Motion Photos and may not work with motion photo formats from other manufacturers.
