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

	/*------*/

	prom := getPrometheusReceiver(logger)
	prom.SetOnNewDataCallBack(func(data map[string]*receiver.CelestiaNode) {

		for _, d := range data {
			mt.AddNodeData(&models.CelestiaNode{
				NodeId:           d.ID,
				NodeType:         d.Type,
				LastPfdTimestamp: d.LastPfdTimestamp,
				PfdCount:         d.PfdCount,
				Head:             d.Head,
			})
		}

		logger.Info(fmt.Sprintf("%d data points stored in db", len(data)))
	})

	prom.Init()

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
		return receiver.NewPrometheusReceiver("0", 10, logger, true)
	}

	promURL := os.Getenv("PROMETHEUS_URL")
	if promURL == "" {
		logger.Fatal("`PROMETHEUS_URL` is empty")
	}

	promIntervalStr := os.Getenv("PROMETHEUS_SYNC_INTERVAL")
	if promIntervalStr == "" {
		logger.Warn("`PROMETHEUS_SYNC_INTERVAL` is empty")
		promIntervalStr = "60"
	}

	promInterval, err := strconv.ParseUint(promIntervalStr, 10, 64)
	if err != nil {
		logger.Fatal(fmt.Sprintf("`PROMETHEUS_SYNC_INTERVAL` env: %v", err))
	}

	return receiver.NewPrometheusReceiver(promURL, promInterval, logger, false)
}
