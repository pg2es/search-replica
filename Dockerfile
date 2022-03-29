FROM golang:1.16-alpine AS prep
RUN apk add --no-cache ca-certificates 
RUN update-ca-certificates

# modules cached in a separate layer
WORKDIR /go/src/search-replica/
COPY go.mod go.sum ./
RUN go mod download

# build static binary
COPY . .
ARG COMMIT="develop"
ARG ARCH="amd64"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
    go build -o /bin/pg2es -a -ldflags "-w -X main.Version=${COMMIT}" ./

# This results in a single layer image
FROM scratch
COPY --from=prep /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=prep /bin/pg2es /bin/pg2es
ENTRYPOINT ["/bin/pg2es"]
