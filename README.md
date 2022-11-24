# nodelogger

Receive nodes telemetry from a Prometheus endpoint and stores them in a Database

## How to run it

First we need to set up the following items:

- A validator node
- A Celestia node (bridge) _with metrics enabled_
- An OTEL collector instance listening to the Celestia node
- A Prometheus instance collecting data from the OTEL

The easiest way at the moment is to use this [bundle](https://github.com/mojtaba-esk/celestia-local)

Then we need to configure our leaderboard backend with the env vars listed in the following.

## Environment Variables

```bash
LOG_LEVEL="info" # {debug|info|warn|error|panic|fatal} defaults to info

PROMETHEUS_URL="http://localhost:9090" # endpoint that Prometheus is running on
PROMETHEUS_SYNC_INTERVAL=30 # seconds

REST_API_ADDRESS=":5050" # port that the leaderboard-backend REST API will run on
API_ROWS_PER_PAGE=100

# database configs
POSTGRES_DB=nodelogger
POSTGRES_USER=root
POSTGRES_PASSWORD=password
POSTGRES_PORT=5432
POSTGRES_HOST=localhost

DEMO="true"  # enables demo mode
```

## API Documentation

_To be done._

Here is a list of available endpoints:

```sh
/api/v1/metrics/nodes
/api/v1/metrics/nodes/bridge
/api/v1/metrics/nodes/full
/api/v1/metrics/nodes/light
/api/v1/metrics/nodes/{id}
```
