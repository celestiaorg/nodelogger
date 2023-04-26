FROM golang:alpine AS development
ARG arch=x86_64

# ENV CGO_ENABLED=0
WORKDIR /go/src/app/
COPY . /go/src/app/

RUN set -eux; \
    apk add --no-cache \
    git \
    openssh \
    ca-certificates \
    build-base

# We need these configs to handle the private repos
# If you do not have the my_keys directory it will be ignored 
# and will raise an error if a private repo found
# get your keys copied in the `my_keys` directory: 
#   mkdir -p ./my_keys && mkdir -p ./my_keys/.ssh
#   cp ~/.ssh/id_rsa ./my_keys/.ssh && cp ~/.gitconfig ./my_keys
COPY . /go/src/app/
COPY main.go my_keys/.ssh/id_rsa* /root/.ssh/
COPY main.go my_keys/.gitconfig* /root/
RUN chmod 700 /root/* \
    && echo -e "\nHost github.com\n\tStrictHostKeyChecking no\n" >> /root/.ssh/config \
    && cd ~ \
    && git config --global url.ssh://git@github.com/.insteadOf https://github.com/

ENV GOPRIVATE=github.com/celestiaorg/leaderboard-backend
RUN mkdir -p /build/ && go build -mod=readonly -buildvcs=false -o /build/app . 

ENV PATH=$PATH:/build
ENTRYPOINT ["tail", "-f", "/dev/null"]

#----------------------------#

FROM alpine:latest AS production

WORKDIR /app/
COPY --from=development /build/* .

ENTRYPOINT ["./app","start"]