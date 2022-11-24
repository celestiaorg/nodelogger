package models

import (
	"time"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"gorm.io/gorm"
)

type CelestiaNode struct {
	gorm.Model
	NodeId           string `gorm:"index;type:varchar(255);not null"`
	NodeType         receiver.NodeType
	LastPfdTimestamp time.Time
	PfdCount         uint64
	Head             uint64
}
