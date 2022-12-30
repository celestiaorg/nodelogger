#!/bin/bash

set -o errexit -o nounset

# Read common EVN vars
export $(cat ../.env | sed '/^#/d' | xargs)

export LOG_LEVEL="info"

export PROMETHEUS_URL="http://localhost:9090"
export PROMETHEUS_SYNC_INTERVAL=30  #seconds

# export EXEC_PATH=./

export API_ROWS_PER_PAGE=100
export REST_API_ADDRESS=":5050"

export DEMO="true"

#--------------------------#
# For dev only

export GOPRIVATE=github.com/celestiaorg/leaderboard-backend

reset && go mod tidy && go build -o app . && ./app
