FROM golang:1.18.4 as builder
LABEL maintainer="SkynetLabs <devs@skynetlabs.com>"

WORKDIR /root

ENV CGO_ENABLED=0

COPY . .

RUN go mod download && make release

FROM alpine:3.16.1
LABEL maintainer="SkynetLabs <devs@skynetlabs.com>"

COPY --from=builder /go/bin/siacoin-promoter /usr/bin/siacoin-promoter

ENTRYPOINT ["siacoin-promoter"]