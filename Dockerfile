ARG GOVERSION=1.17
ARG BUILDPLATFORM="linux/amd64"
FROM --platform=$BUILDPLATFORM golang:${GOVERSION}-alpine AS build
RUN apk add --no-cache ca-certificates 
RUN update-ca-certificates

# modules cached in a separate layer
WORKDIR /go/src/search-replica/
COPY go.mod go.sum ./
RUN go mod download

# build static binary
COPY . .
ARG COMMIT="develop"
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -o /bin/pg2es -a -ldflags "-w -X main.Version=${COMMIT}" ./

# This results in a single layer image
FROM scratch
# It's important to accept same arguments here.
# Otherwise cache is broken, and all architectures will get same binary
ARG TARGETOS TARGETARCH
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /bin/pg2es /bin/pg2es
ENTRYPOINT ["/bin/pg2es"]
