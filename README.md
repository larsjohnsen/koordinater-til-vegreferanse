# Koordinater til Vegreferanse

A Go application that provides bidirectional conversion between UTM33 coordinates and Norwegian road references (vegreferanse) using the Norwegian Public Roads Administration (NVDB) API v4.

## Features

- Bidirectional conversion between UTM33 coordinates and vegreferanse:
  - Convert UTM33 coordinates to vegreferanse
  - Convert vegreferanse to UTM33 coordinates
- Handles rate limiting and efficient caching to reduce API calls
- Supports multiple concurrent workers for high-performance processing
- Intelligently maintains travel continuity when multiple road matches are available
- Provides a summary of road numbers with their corresponding row ranges in the input file

## Usage

The Go runtime, at least version 1.25, must be installed and available in $PATH.

```bash
# Convert coordinates to vegreferanse (coord_to_vegref mode)
go run . -mode=coord_to_vegref -input=input/data.txt -output=output/result.txt -x-column=2 -y-column=3

# Convert vegreferanse to coordinates (vegref_to_coord mode)
go run . -mode=vegref_to_coord -input=input/vegrefs.txt -output=output/coords.txt -vegreferanse-column=6

# With additional settings
go run . -mode=coord_to_vegref -input=data/myfile.txt -output=results/output.txt -x-column=2 -y-column=3 \
  -cache-dir=./my_cache -rate-limit=40 -workers=10 -max-distance=15
```

### Command-line flags

#### Common flags (required)
| Flag     | Description                                  |
|----------|----------------------------------------------|
| -mode    | **Required**. Conversion mode: coord_to_vegref or vegref_to_coord |
| -input   | **Required**. Input file path                |
| -output  | **Required**. Output file path               |

#### Mode-specific flags
| Flag                  | Mode           | Description                                  |
|-----------------------|----------------|----------------------------------------------|
| -x-column             | coord_to_vegref| **Required**. 0-based index of the column containing X coordinates |
| -y-column             | coord_to_vegref| **Required**. 0-based index of the column containing Y coordinates |
| -vegreferanse-column  | vegref_to_coord| **Required**. 0-based index of the column containing vegreferanse |

#### Optional flags
| Flag           | Default               | Description                                  |
|----------------|----------------------|----------------------------------------------|
| -no-cache      | false                | Disable disk cache                           |
| -cache-dir     | cache/api_responses  | Directory for disk cache                     |
| -clear-cache   | false                | Clear existing cache before starting         |
| -max-distance  | 10                   | Maximum distance in meters for filtering API results |
| -rate-limit    | 40                   | Number of API calls allowed per time frame   |
| -rate-time     | 1000                 | Rate limit time frame in milliseconds        |
| -workers       | 5                    | Number of concurrent workers                 |

## Input/Output Format

### Coordinates to Vegreferanse Mode (coord_to_vegref)
- **Input**: Tab-delimited file with a header row and X/Y coordinates in UTM33 format
- **Output**: Same as input with an additional column for vegreferanse

### Vegreferanse to Coordinates Mode (vegref_to_coord)
- **Input**: Tab-delimited file with a header row and a vegreferanse column
- **Output**: Same as input with two additional columns for X and Y coordinates in UTM33 format