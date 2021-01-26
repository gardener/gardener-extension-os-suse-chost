############# builder
FROM eu.gcr.io/gardener-project/3rd/golang:1.15.7 AS builder

WORKDIR /go/src/github.com/gardener/gardener-extension-os-suse-chost
COPY . .
RUN make install-requirements && make generate && make install

############# gardener-extension-os-suse-chost
FROM eu.gcr.io/gardener-project/3rd/alpine:3.12.3 AS gardener-extension-os-suse-chost

COPY --from=builder /go/bin/gardener-extension-os-suse-chost /gardener-extension-os-suse-chost
ENTRYPOINT ["/gardener-extension-os-suse-chost"]
