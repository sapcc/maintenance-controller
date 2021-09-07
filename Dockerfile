ARG build_img=golang:1.16
ARG distroless_img=gcr.io/distroless/static:nonroot

# Build the manager binary
FROM $build_img as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
# COPY api/ api/
COPY controllers/ controllers/
COPY plugin/ plugin/
COPY state/ state/
COPY esx/ esx/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM $distroless_img
WORKDIR /
LABEL source_repository="https://github.com/sapcc/maintenance-controller"
COPY --from=builder /workspace/manager .
USER nonroot:nonroot

ENTRYPOINT ["/manager"]
