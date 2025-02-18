#!/bin/bash

# Building the binary that is going to be released 
GOOS=linux GOARCH=amd64 go build -o bin/app-amd64-linux cmd/general-service/main.go  

# Changing the name of the binary
mv  bin/app-amd64-linux bin/general-service
