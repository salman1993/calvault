# calvault build targets

version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
commit := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
build_date := `date -u +"%Y-%m-%dT%H:%M:%SZ"`

ldflags := "-X github.com/salman1993/calvault/cmd/calvault/cmd.Version=" + version + " -X github.com/salman1993/calvault/cmd/calvault/cmd.Commit=" + commit + " -X github.com/salman1993/calvault/cmd/calvault/cmd.BuildDate=" + build_date

# Show available targets
default:
    @just --list

# Build the binary (debug)
build:
    CGO_ENABLED=1 go build -ldflags="{{ldflags}}" -o calvault ./cmd/calvault
    @chmod +x calvault

# Install to ~/.local/bin
install:
    CGO_ENABLED=1 go build -ldflags="{{ldflags}}" -o ~/.local/bin/calvault ./cmd/calvault

# Clean build artifacts
clean:
    rm -f calvault

# Run tests
test:
    go test ./...

# Run tests with verbose output
test-v:
    go test -v ./...

# Format code
fmt:
    go fmt ./...

# Run linter
lint:
    golangci-lint run ./...

# Tidy dependencies
tidy:
    go mod tidy
