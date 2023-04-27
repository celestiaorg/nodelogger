package models

import (
	"time"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"gorm.io/gorm"
)

type CelestiaNode struct {
	gorm.Model
	NodeId                                      string `gorm:"index;type:varchar(255);not null"`
	NodeType                                    receiver.NodeType
	LastPfbTimestamp                            time.Time
	PfbCount                                    uint64
	Head                                        uint64
	NetworkHeight                               uint64 `gorm:"index;"`
	DasLatestSampledTimestamp                   time.Time
	DasNetworkHead                              uint64
	DasSampledChainHead                         uint64
	DasSampledHeadersCounter                    uint64
	DasTotalSampledHeaders                      uint64
	TotalSyncedHeaders                          uint64
	StartTime                                   time.Time
	LastRestartTime                             time.Time
	NodeRuntimeCounterInSeconds                 uint64
	LastAccumulativeNodeRuntimeCounterInSeconds uint64
	Uptime                                      float32
	NewUptime                                   float32
}
