package cmd

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/database"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
)

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

func getUptimeStartTime(logger *zap.Logger) time.Time {

	uptimeStartTimeStr := os.Getenv("UPTIME_START_TIME")
	if uptimeStartTimeStr == "" {
		logger.Fatal("`UPTIME_START_TIME` is empty")
	}
	uptimeStartTime, err := time.Parse(time.RFC3339, uptimeStartTimeStr)
	if err != nil {
		logger.Fatal(fmt.Sprintf("Error parsing `UPTIME_START_TIME` date: %v", err))
	}

	return uptimeStartTime
}

func getPrometheusReceiver(logger *zap.Logger) *receiver.PrometheusReceiver {

	if getDemoMode() {
		logger.Info("Demo mode activated")
		return receiver.NewPrometheusReceiver("0", 10, logger, "", 0, time.Now().Add(-10*24*time.Hour), true)
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

	uptimeStartTime := getUptimeStartTime(logger)

	return receiver.NewPrometheusReceiver(promURL, promInterval, logger, promNSprefix, 0, uptimeStartTime, false)
}

func getTendermintReceiver(logger *zap.Logger) *receiver.TendermintReceiver {

	if getDemoMode() {
		return receiver.NewTendermintReceiver("", 0, 0, 3600, "", logger, true)
	}

	rpcAddr := os.Getenv("APP_TM_RPC")
	if rpcAddr == "" {
		logger.Fatal("`APP_TM_RPC` is empty")
	}

	return receiver.NewTendermintReceiver(rpcAddr, 10, 10, 10, "", logger, false)
}

func getDemoMode() bool {
	return os.Getenv("DEMO") == "true"
}

func getDatabase(logger *zap.Logger) *gorm.DB {

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

	return db
}
