############# builder
FROM golang:1.27.4@sha256:5f1b98fa02fe396ebce2a0227a0e08309e11792915b9dd2e1f71053d7e7e6666 AS builder

WORKDIR /go/src/github.com/gardener/gardener-extension-os-suse-chost
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make install

############# gardener-extension-os-suse-chost
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5 AS gardener-extension-os-suse-chost
WORKDIR /

COPY --from=builder /go/bin/gardener-extension-os-suse-chost /gardener-extension-os-suse-chost
ENTRYPOINT ["/gardener-extension-os-suse-chost"]
