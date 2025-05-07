############# builder
FROM golang:1.24.3 AS builder

WORKDIR /go/src/github.com/gardener/gardener-extension-os-suse-chost
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make install

############# gardener-extension-os-suse-chost
FROM gcr.io/distroless/static-debian11:nonroot AS gardener-extension-os-suse-chost
WORKDIR /

COPY --from=builder /go/bin/gardener-extension-os-suse-chost /gardener-extension-os-suse-chost
ENTRYPOINT ["/gardener-extension-os-suse-chost"]
