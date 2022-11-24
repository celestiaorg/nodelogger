package metrics

import (
	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/database/models"
	"gorm.io/gorm"
)

type Metrics struct {
	db *gorm.DB
}

const defaultLimit = 100

func New(db *gorm.DB) *Metrics {
	return &Metrics{
		db: db,
	}
}

func (m *Metrics) AddNodeData(data *models.CelestiaNode) error {
	tx := m.db.Create(data)
	return tx.Error
}

func (m *Metrics) FindByNodeId(nodeId string, offset, limit int) ([]models.CelestiaNode, int64, error) {

	var res []models.CelestiaNode

	var count int64
	if limit == 0 {
		limit = defaultLimit
	}
	tx := m.db.Model(&models.CelestiaNode{}).Where(&models.CelestiaNode{NodeId: nodeId}).Count(&count)
	if tx.Error != nil {
		return res, count, tx.Error
	}

	tx = m.db.Offset(offset).Limit(limit).Where(&models.CelestiaNode{NodeId: nodeId}).Find(&res)
	return res, count, tx.Error
}

func (m *Metrics) GetAllNodes(offset, limit int) ([]models.CelestiaNode, int64, error) {

	var res []models.CelestiaNode

	var count int64
	if limit == 0 {
		limit = defaultLimit
	}
	tx := m.db.Model(&models.CelestiaNode{}).Count(&count)
	if tx.Error != nil {
		return res, count, tx.Error
	}

	tx = m.db.Offset(offset).Limit(limit).Find(&res)
	return res, count, tx.Error
}

func (m *Metrics) GetNodesByType(nType receiver.NodeType, offset, limit int) ([]models.CelestiaNode, int64, error) {

	var res []models.CelestiaNode

	var count int64
	if limit == 0 {
		limit = defaultLimit
	}
	tx := m.db.Model(&models.CelestiaNode{}).Where(&models.CelestiaNode{NodeType: nType}).Count(&count)
	if tx.Error != nil {
		return res, count, tx.Error
	}

	tx = m.db.Offset(offset).Limit(limit).Where(&models.CelestiaNode{NodeType: nType}).Find(&res)
	return res, count, tx.Error
}
