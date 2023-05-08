package metrics

import (
	"fmt"

	"github.com/celestiaorg/nodelogger/database"
	"github.com/celestiaorg/nodelogger/database/models"
)

func (m *Metrics) GetNodesVersions(nodeId string) ([]models.NodeVersion, error) {

	var rows []models.NodeVersion

	SQL := fmt.Sprintf(`
		SELECT 
			"node_id","version", "created_at" 
		FROM "celestia_nodes" 
		WHERE 
			"node_id" = '%s'
			AND "version" != ''
		ORDER BY 
			"created_at" DESC`, nodeId)
	if err := database.Query(m.db, SQL, &rows); err != nil {
		return rows, err
	}
	return rows, nil

}
