#!/bin/bash

# Watchtower E2E Build Script
# Builds a local watchtower image for end-to-end testing
# This replaces the old wt.sh script with a more streamlined version

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DOCKERFILE="$REPO_ROOT/build/docker/Dockerfile.self-local"

# Default values
IMAGE_NAME="${WATCHTOWER_E2E_IMAGE:-watchtower:test}"
TAG="${WATCHTOWER_E2E_TAG:-latest}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date '+%Y-%m-%d %H:%M:%S')] $1${NC}"
}

error() {
    echo -e "${RED}[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: $1${NC}" >&2
}

warn() {
    echo -e "${YELLOW}[$(date '+%Y-%m-%d %H:%M:%S')] WARNING: $1${NC}"
}

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."

    if ! command -v docker &> /dev/null; then
        error "Docker is not installed or not in PATH"
        exit 1
    fi

    if ! docker info &> /dev/null; then
        error "Docker daemon is not running"
        exit 1
    fi

    if [ ! -f "$DOCKERFILE" ]; then
        error "Dockerfile not found: $DOCKERFILE"
        exit 1
    fi

    log "Prerequisites check passed"
}

# Build the watchtower image
build_image() {
    log "Building Watchtower image..."
    log "Image: $IMAGE_NAME:$TAG"
    log "Dockerfile: $DOCKERFILE"
    log "Context: $REPO_ROOT"

    if ! docker build -f "$DOCKERFILE" -t "$IMAGE_NAME:$TAG" "$REPO_ROOT"; then
        error "Failed to build Watchtower image"
        exit 1
    fi

    log "Watchtower image built successfully: $IMAGE_NAME:$TAG"
}

# Verify the build
verify_build() {
    log "Verifying build..."

    # Check if image exists
    if ! docker image inspect "$IMAGE_NAME:$TAG" &> /dev/null; then
        error "Built image not found: $IMAGE_NAME:$TAG"
        exit 1
    fi

    # Try to run the image briefly to check it's functional
    log "Testing image functionality..."
    if ! docker run --rm "$IMAGE_NAME:$TAG" --help &> /dev/null; then
        error "Image verification failed - --help command failed"
        exit 1
    fi

    log "Image verification passed"
}

# Display usage information
usage() {
    cat << EOF
Watchtower E2E Build Script

Builds a local Watchtower image for end-to-end testing.

Usage: $0 [OPTIONS]

Options:
    -i, --image NAME    Docker image name (default: watchtower:test)
    -t, --tag TAG       Docker image tag (default: latest)
    -h, --help          Show this help message

Environment Variables:
    WATCHTOWER_E2E_IMAGE    Docker image name (same as --image)
    WATCHTOWER_E2E_TAG      Docker image tag (same as --tag)

Examples:
    $0                                    # Build watchtower:test
    $0 --image my-watchtower --tag dev    # Build my-watchtower:dev
    WATCHTOWER_E2E_IMAGE=custom $0        # Use environment variable

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -i|--image)
            IMAGE_NAME="$2"
            shift 2
            ;;
        -t|--tag)
            TAG="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Override with environment variables if set
IMAGE_NAME="${WATCHTOWER_E2E_IMAGE:-$IMAGE_NAME}"
TAG="${WATCHTOWER_E2E_TAG:-$TAG}"

# Main execution
main() {
    log "Starting Watchtower E2E build process"
    log "Target image: $IMAGE_NAME:$TAG"

    check_prerequisites
    build_image
    verify_build

    log "✅ Watchtower E2E build completed successfully!"
    log "Image ready for testing: $IMAGE_NAME:$TAG"
    echo ""
    log "Run E2E tests with:"
    echo "  go test ./test/e2e/... -v"
    echo ""
    log "Or run specific test suites:"
    echo "  go test ./test/e2e/suites/ -v"
    echo "  go test ./test/e2e/scenarios/git/ -v"
    echo "  go test ./test/e2e/scenarios/registry/ -v"
}

# Run main function
main "$@"
