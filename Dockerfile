# SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
#
# SPDX-License-Identifier: Apache-2.0

# Build the manager binary
FROM golang:1.27-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

COPY ./ /workspace/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on GOTOOLCHAIN=local go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
LABEL source_repository="https://github.com/sapcc/maintenance-controller"
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/static static
USER nonroot:nonroot

ENTRYPOINT ["/manager"]
