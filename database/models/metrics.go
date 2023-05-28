package models

import (
	"time"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"gorm.io/gorm"
)

type CelestiaNode struct {
	// gorm.Model:
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"index"`
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
	// ----------
	NodeId                                      string `gorm:"index;type:varchar(255);not null"`
	NodeType                                    receiver.NodeType
	Version                                     string `gorm:"index;type:varchar(255);"`
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
	NewRuntime                                  int64
}
type NodeVersion struct {
	NodeId    string    `json:"node_id"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}
