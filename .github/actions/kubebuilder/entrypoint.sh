#!/bin/sh
cd $GITHUB_WORKSPACE
KUBEBUILDER_ASSETS=/kubebuilder/kubebuilder_2.3.1_linux_amd64/bin go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
