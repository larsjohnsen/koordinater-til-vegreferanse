# Koordinater til Vegreferanse

A Go application that reads UTM33 coordinates from tabulated files, converts them to Norwegian road references (vegreferanse) using the Norwegian Public Roads Administration (NVDB) API v4, and appends the results in a new column labeled `Vegreferanse`.

## Features

- Intelligently maintains travel continuity when multiple road matches are available.

## Usage

```bash
# Basic usage with default settings
go run .

# With custom settings
go run . -cache-dir=./my_cache -radius=15 -rate-limit=40 -workers=10

# Process a specific file with custom coordinate columns
go run . -input=data/myfile.txt -output=results/output.txt -x-column=2 -y-column=3
```

### Command-line flags

| Flag           | Default               | Description                                  |
|----------------|----------------------|----------------------------------------------|
| -no-cache      | false                | Disable disk cache                           |
| -cache-dir     | cache/api_responses  | Directory for disk cache                     |
| -clear-cache   | false                | Clear existing cache before starting         |
| -radius        | 10                   | Search radius in meters                      |
| -rate-limit    | 40                   | Number of API calls allowed per time frame   |
| -rate-time     | 1000                 | Rate limit time frame in milliseconds        |
| -workers       | 5                    | Number of concurrent workers                 |
| -input         | input/7834.txt       | Input file path                              |
| -output        | output/7834_with_vegreferanse.txt | Output file path                |
| -x-column      | 4                    | 0-based index of the column containing X coordinates |
| -y-column      | 5                    | 0-based index of the column containing Y coordinates |

## Input/Output Format

- **Input**: Tab-delimited file with a header row and X/Y coordinates in UTM33 format
- **Output**: Same as input with an additional column for vegreferanse