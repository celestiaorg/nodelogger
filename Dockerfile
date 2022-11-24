FROM golang:alpine AS development
ARG arch=x86_64

# ENV CGO_ENABLED=0
WORKDIR /go/src/app/
COPY . /go/src/app/

RUN mkdir -p /build/ && go build -mod=readonly -buildvcs=false -o /build/app . 

ENV PATH=$PATH:/build
ENTRYPOINT ["tail", "-f", "/dev/null"]

#----------------------------#

FROM alpine:latest AS production

WORKDIR /app/
COPY --from=development /build .

ENTRYPOINT ["./app"]