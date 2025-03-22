# Go Motion Photo

A command-line tool for handling **Samsung Motion Photos**. Extract video and image components from motion photo files (`.jpg`, `.jpeg`, `.heic`).

## Features

- Process Samsung Motion Photos (both legacy and current formats)
- Regular expression support for file matching
- Configurable output options
- Supports JPG and HEIC motion photo formats
- Configurable logging system

## Options

### Input Options
- `--input <path>`: Path to motion photo file or directory
    - Single file: `photo.jpg`
    - Directory: `./photos`
    - Regex pattern: `/IMG_\d{4}\.jpg/`
    - Glob pattern: `*.jpg`
    - Supported formats: `.jpg`, `.jpeg`, `.heic`

### Output Options
- `--output <dir>`: Output directory for extracted files (default: current directory)
- `--delete-orig`: Remove original file after successful extraction
- `--rename-orig`: Use base name for extracted files, add `_original` to source file
- `--extract-photo`: Extract photo component (default: true)
- `--force`: Overwrite existing output files

### Logging Options
- `--log-file <path>`: Log file path
- `--log-level <level>`: Log level (`debug`, `info`, `warn`, `error`)
- `--no-console-log`: Disable console output

## Configuration

Configuration can be provided through:
- Command line arguments
- Configuration file (`go-motion-photo.yaml`)
- Environment variables

Default config locations:
- Current directory
- `$HOME/.config/go-motion-photo`

## Usage Examples

Using command-line flags:
```bash
# Process single file
go-motion-photo photo.jpg

# Specify output location
go-motion-photo --input photo.jpg --output ./extracted

# Process all supported files in directory
go-motion-photo --input ./photos

# Process files matching regex pattern
go-motion-photo --input /IMG_\d{4}\.jpg/

# Keep original naming scheme
go-motion-photo --input photo.jpg --rename-orig

# Process HEIC file and overwrite existing outputs
go-motion-photo --input photo.heic --force
```

Using configuration file (`go-motion-photo.yaml`):
```yaml
input: "./photos"
output: "./extracted"
delete_orig: false
rename_orig: true
log:
  file: "motion-photo.log"
  level: "info"
  no_console: false
```

## Installation

Binary releases are available for Linux, macOS, and Windows on the [releases page](https://github.com/ikerls/motion-photo-extractor/releases).

Note: This tool is specifically designed for Samsung Motion Photos and may not work with motion photo formats from other manufacturers.
