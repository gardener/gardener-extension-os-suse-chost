############# builder
FROM golang:1.13.4 AS builder

WORKDIR /go/src/github.com/gardener/gardener-extension-os-suse-jeos
COPY . .
RUN make install-requirements && make VERIFY=false all

############# gardener-extension-os-suse-jeos
FROM alpine:3.11.3 AS gardener-extension-os-suse-jeos

COPY --from=builder /go/bin/gardener-extension-os-suse-jeos /gardener-extension-os-suse-jeos
ENTRYPOINT ["/gardener-extension-os-suse-jeos"]
