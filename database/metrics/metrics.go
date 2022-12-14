package metrics

import (
	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
			Column: clause.Column{Name: "pfd_count"},
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
			Column: clause.Column{Name: "pfd_count"},
			Desc:   true,
		}).Find(&res)
	return res, count, tx.Error
}

func (m *Metrics) GetNodeUpTime(nodeId string) (float32, error) {

	var nodeInfo models.CelestiaNode
	tx := m.db.Where(&models.CelestiaNode{NodeId: nodeId}).Last(&nodeInfo)
	if tx.Error != nil {
		return 0, tx.Error
	}

	// We might need to omit the time check as this code might be running
	// long time after the incentivized testnet
	// lastTwoHours := time.Now().Add(-2 * time.Hour)
	// if nodeInfo.LastPfdTimestamp.After(lastTwoHours) {
	// 	return 0, nil
	// }

	var topNode models.CelestiaNode
	tx = m.db.
		Where(&models.CelestiaNode{NodeType: nodeInfo.NodeType}).
		Order(clause.OrderByColumn{
			Column: clause.Column{Name: "pfd_count"},
			Desc:   true,
		}).First(&topNode)

	if tx.Error != nil {
		return 0, tx.Error
	}
	if topNode.PfdCount == 0 {
		return 0, nil
	}

	// Calculate the position of the given node wrt the top node's performance (as expected PFD)
	performance := float64(nodeInfo.PfdCount) / float64(topNode.PfdCount)

	return float32(performance), nil
}
