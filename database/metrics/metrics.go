package metrics

import (
	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Metrics struct {
	db          *gorm.DB
	InsertQueue *InsertQueue
}

const defaultLimit = 100

func New(db *gorm.DB) *Metrics {
	m := &Metrics{
		db: db,
	}
	m.InsertQueue = NewInsertQueue(m)
	return m
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

	tx = m.db.Offset(offset).Limit(limit).
		Where(&models.CelestiaNode{NodeId: nodeId}).Find(&res)
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

	tx = m.db.Offset(offset).Limit(limit).
		Order(clause.OrderByColumn{
			Column: clause.Column{Name: "uptime"},
			Desc:   true,
		}).Find(&res)
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

	tx = m.db.Offset(offset).Limit(limit).
		Where(&models.CelestiaNode{NodeType: nType}).
		Order(clause.OrderByColumn{
			Column: clause.Column{Name: "uptime"},
			Desc:   true,
		}).Find(&res)
	return res, count, tx.Error
}

func (m *Metrics) FindByNodeIdAtNetworkHeight(nodeId string, networkHeight uint64) ([]models.CelestiaNode, error) {

	var res []models.CelestiaNode

	tx := m.db.Where(&models.CelestiaNode{NodeId: nodeId, NetworkHeight: networkHeight}).Find(&res)
	return res, tx.Error
}

func (m *Metrics) FindByNodeIdAtNetworkHeightRange(nodeId string, networkHeightBegin, networkHeightEnd uint64) ([]models.CelestiaNode, error) {

	var res []models.CelestiaNode

	tx := m.db.Where("node_id = ? AND ( network_height BETWEEN ? AND ? )", nodeId, networkHeightBegin, networkHeightEnd).Find(&res)
	return res, tx.Error
}
