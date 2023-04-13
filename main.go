package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/api/v1"
	"github.com/celestiaorg/nodelogger/database"
	"github.com/celestiaorg/nodelogger/database/metrics"
	"github.com/celestiaorg/nodelogger/database/models"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {

	logger, err := getLogger()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	/*------*/

	psqlconn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("POSTGRES_HOST"),
		os.Getenv("POSTGRES_PORT"),
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("POSTGRES_DB"),
	)

	db, err := database.Init(psqlconn)
	if err != nil {
		logger.Fatal(fmt.Sprintf("database initialization: %v", err))
	}

	/*------*/

	mt := metrics.New(db)
	mt.InsertQueue.Start()

	/*------*/

	prom := getPrometheusReceiver(logger)
	prom.SetOnNewDataCallBack(func(node *receiver.CelestiaNode) {

		mt.InsertQueue.Add(&models.CelestiaNode{
			NodeId:                      node.ID,
			NodeType:                    node.Type,
			LastPfbTimestamp:            node.LastPfbTimestamp,
			PfbCount:                    node.PfbCount,
			Head:                        node.Head,
			NetworkHeight:               node.NetworkHeight,
			DasLatestSampledTimestamp:   node.DasLatestSampledTimestamp,
			DasNetworkHead:              node.DasNetworkHead,
			DasSampledChainHead:         node.DasSampledChainHead,
			DasSampledHeadersCounter:    node.DasSampledHeadersCounter,
			DasTotalSampledHeaders:      node.DasTotalSampledHeaders,
			TotalSyncedHeaders:          node.TotalSyncedHeaders,
			StartTime:                   node.StartTime,
			LastRestartTime:             node.LastRestartTime,
			NodeRuntimeCounterInSeconds: node.NodeRuntimeCounterInSeconds,
			LastAccumulativeNodeRuntimeCounterInSeconds: node.LastAccumulativeNodeRuntimeCounterInSeconds,
			Uptime: node.Uptime,
		})

	})
	// logger.Info(fmt.Sprintf("%d data points stored in db", len(data)))

	// The tendermint receiver is needed to get the network height
	// as we do not collect data about validators, it does not need to be initiated
	tm := getTendermintReceiver(logger)

	re := receiver.New(prom, nil, tm, logger)
	// Only prometheus receiver service need to be initiated
	re.InitPrometheus()

	/*------*/

	restApi := api.NewRESTApiV1(mt, logger)

	addr := os.Getenv("REST_API_ADDRESS")
	if addr == "" {
		logger.Fatal("`REST_API_ADDRESS` is empty")
	}

	originAllowed := os.Getenv("ORIGIN_ALLOWED")
	if originAllowed == "" {
		logger.Fatal("`ORIGIN_ALLOWED` is empty")
	}

	logger.Fatal(fmt.Sprintf("REST API server: %v", restApi.Serve(addr, originAllowed)))
}

func getLogger() (*zap.Logger, error) {

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	var cfg zap.Config

	if os.Getenv("PRODUCTION_MODE") == "true" {
		cfg = zap.NewProductionConfig()
		cfg.OutputPaths = []string{"stdout"}
		cfg.ErrorOutputPaths = []string{"stderr"}
		cfg.Encoding = "console" // "console" | "json"

	} else {
		cfg = zap.NewDevelopmentConfig()
		// Use only with console encoder (i.e. not in production)
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	var err error
	cfg.Level, err = zap.ParseAtomicLevel(logLevel)
	if err != nil {
		return nil, fmt.Errorf("getLogger: %v", err)
	}

	return cfg.Build()
}

func getPrometheusReceiver(logger *zap.Logger) *receiver.PrometheusReceiver {

	if os.Getenv("DEMO") == "true" {
		logger.Info("Demo mode activated")
		return receiver.NewPrometheusReceiver("0", 10, logger, "", 0, true)
	}

	promURL := os.Getenv("PROMETHEUS_URL")
	if promURL == "" {
		logger.Fatal("`PROMETHEUS_URL` is empty")
	}

	promNSprefix := os.Getenv("PROMETHEUS_NAMESPACE_PREFIX") // This can be empty

	promIntervalStr := os.Getenv("PROMETHEUS_SYNC_INTERVAL")
	if promIntervalStr == "" {
		logger.Warn("`PROMETHEUS_SYNC_INTERVAL` is empty")
		promIntervalStr = "60"
	}

	promInterval, err := strconv.ParseUint(promIntervalStr, 10, 64)
	if err != nil {
		logger.Fatal(fmt.Sprintf("`PROMETHEUS_SYNC_INTERVAL` env: %v", err))
	}

	return receiver.NewPrometheusReceiver(promURL, promInterval, logger, promNSprefix, 0, false)
}

func getTendermintReceiver(logger *zap.Logger) *receiver.TendermintReceiver {

	if os.Getenv("DEMO") == "true" {
		return receiver.NewTendermintReceiver("", 0, 0, 3600, "", logger, true)
	}

	rpcAddr := os.Getenv("APP_TM_RPC")
	if rpcAddr == "" {
		logger.Fatal("`APP_TM_RPC` is empty")
	}

	return receiver.NewTendermintReceiver(rpcAddr, 10, 10, 10, "", logger, false)
}
