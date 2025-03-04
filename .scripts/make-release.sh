#!/bin/bash

# Exit script on any error
set -e

# Build the binary that is going to be released 
GOOS=linux GOARCH=amd64 go build -o general-service cmd/general-service/main.go  

# Add the binary to git tracking
git add general-service

# Ensure that we are up to date with remote 
git pull origin main

# Get the current date in the desired format
current_date=$(date +"%Y.%m.%d")

# Get the current commit hash (shortened)
commit_hash=$(git rev-parse --short HEAD)

# Variable for the tag
tag="v${current_date}-sha${commit_hash}"

# Commit the binary with a message (if there are changes)
git commit -m "Add compiled binary for release $tag"

# Output the future release tag 
echo "The version tag is: $tag"

# Create and push the tag
git tag -a "$tag" -m "Release $tag"
git push origin "$tag"

echo "Tag $tag created and pushed successfully."
