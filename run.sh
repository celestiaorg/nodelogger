#!/bin/bash

set -o errexit -o nounset

# Read common EVN vars
export $(cat ../.env | sed '/^#/d' | xargs)

export LOG_LEVEL="info"

export PROMETHEUS_URL="http://localhost:9093"
export PROMETHEUS_SYNC_INTERVAL=30  #seconds

# export APP_TM_RPC="http://localhost:26657"
export APP_TM_RPC="https://rpc-mamaki.pops.one:443"


# export EXEC_PATH=./

export API_ROWS_PER_PAGE=100
export REST_API_ADDRESS=":5052"
# ORIGIN_ALLOWED is like `scheme://dns[:port]`, or `*` (insecure)
export ORIGIN_ALLOWED="*"


export DEMO="false"

#--------------------------#
# For dev only

export GOPRIVATE=github.com/celestiaorg/leaderboard-backend

reset && go mod tidy && go build -o app . && ./app
