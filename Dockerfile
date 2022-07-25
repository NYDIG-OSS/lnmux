FROM golang:1.18-alpine AS build

ARG LEDGER_COMMIT="dirty"
ARG LEDGER_TAG="dev"

COPY . /go/src/github.com/bottlepay/lnmux

RUN cd /go/src/github.com/bottlepay/lnmux \
    && GOOS=linux CGO_ENABLED=0 go install \
        github.com/bottlepay/lnmux/cmd/...

### Build an Alpine image
FROM alpine:3.16 as alpine

# Update CA certs
RUN apk add --no-cache ca-certificates && rm -rf /var/cache/apk/*

# Copy over app binary
COPY --from=build /go/bin/lnmuxd /usr/bin/lnmuxd

# Add a user
RUN mkdir -p /app && adduser -D lnmux && chown -R lnmux /app
USER lnmux

WORKDIR /app/

CMD [ "/usr/bin/lnmuxd" ]

### Build a Debian image
FROM debian:bullseye-slim as debian

# Update CA certs
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy over app binary
COPY --from=build /go/bin/lnmuxd /usr/bin/lnmuxd

# Add a user
RUN mkdir -p /app && adduser --disabled-login lnmux && chown -R lnmux /app
USER lnmux

WORKDIR /app

CMD [ "/usr/bin/lnmuxd" ]