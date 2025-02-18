#!/bin/bash

GOOS=linux GOARCH=amd64 go build -o bin/app-amd64-linux cmd/general-service/main.go  
mv  bin/app-amd64-linux bin/general-service