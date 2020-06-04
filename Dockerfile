############# builder
FROM golang:1.14.4 AS builder

WORKDIR /go/src/github.com/gardener/gardener-extension-os-suse-jeos
COPY . .
RUN make install-requirements && make generate && make install

############# gardener-extension-os-suse-jeos
FROM alpine:3.12.0 AS gardener-extension-os-suse-jeos

COPY --from=builder /go/bin/gardener-extension-os-suse-jeos /gardener-extension-os-suse-jeos
ENTRYPOINT ["/gardener-extension-os-suse-jeos"]
