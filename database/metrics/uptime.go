package metrics

import (
	"fmt"
	"time"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/database"
	"github.com/celestiaorg/nodelogger/database/models"
)

func (m *Metrics) GetNodeUpTime(nodeId string) (float32, error) {

	var nodeInfo models.CelestiaNode
	tx := m.db.Where(&models.CelestiaNode{NodeId: nodeId}).Last(&nodeInfo)
	if tx.Error != nil {
		return 0, tx.Error
	}

	return nodeInfo.Uptime, nil
}

func (m *Metrics) RecomputeUptimeForAll(uptimeStartTime, uptimeEndTime time.Time) ([]models.CelestiaNode, error) {

	nodesList := []models.CelestiaNode{}

	rows := []string{}
	SQL := `SELECT DISTINCT "node_id" from "celestia_nodes"`
	if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
		return nodesList, err
	}

	for i, nodeId := range rows {
		fmt.Printf("[ %d / %d ] nodeId: %v ", i+1, len(rows), nodeId)
		latestNodeData, err := m.GetLatestNodeData(nodeId)
		if err != nil {
			return nodesList, err
		}
		newRunTime, err := m.recomputeRuntime(nodeId, 0, uptimeEndTime)
		if err != nil {
			return nodesList, err
		}
		networkHeight, err := m.getNetworkHeightAtTime(uptimeEndTime)
		if err != nil {
			return nodesList, err
		}

		newUptime := nodeUptime(latestNodeData, uint64(newRunTime), networkHeight, uptimeStartTime)
		fmt.Printf("\toldUptime: %v\tnewUptime: %v\n", latestNodeData.Uptime, newUptime)

		latestNodeData.NewUptime = newUptime
		latestNodeData.LastAccumulativeNodeRuntimeCounterInSeconds = uint64(newRunTime)
		latestNodeData.NodeRuntimeCounterInSeconds = 0 // Since we already calculated it in the newRuntime, this value must be zero

		nodesList = append(nodesList, latestNodeData)
	}

	return nodesList, nil
}

// This one processes everything in the DB and so it avoids transferring huge amount of data to the client and so it is faster
// It stores the outcome in cache and if re-execute it again, it reads the already processed data from cache in sequences
func (m *Metrics) recomputeRuntime(nodeId string, networkHeightBegin uint64, endTime time.Time) (int64, error) {

	startTime := time.Now().Unix()

	var rows []models.CelestiaNode

	latestIdFromCache := uint(0)
	latestRuntimeFromCache := int64(0)

	SQLTxt := `
		SELECT 
			MAX("id") AS "id",
			CAST(ROUND(SUM("time_gap_seconds")::NUMERIC) AS BIGINT) + %d AS "new_runtime"
		FROM (
		SELECT 
			t1."id", 
			t1."node_id", 
			t1."created_at", 
			EXTRACT(EPOCH FROM (MIN(t2."created_at") - t1."created_at")) AS "time_gap_seconds"
		FROM 
			"celestia_nodes" t1 
			LEFT JOIN "celestia_nodes" t2 ON t1.node_id = t2.node_id AND t1."created_at" < t2."created_at" 
		WHERE 
			t1."id" >= %d
			AND t1."network_height" > %d 	
			AND t1."created_at" < '%v'
			AND t1."node_id" = '%s'
		GROUP BY 
			t1."id", t1."node_id", t1."created_at" 
		ORDER BY 
			t1."id" ASC
		) AS subquery
		WHERE "time_gap_seconds" < 100`

	SQL := fmt.Sprintf(SQLTxt, latestRuntimeFromCache, latestIdFromCache, networkHeightBegin, endTime, nodeId)
	for database.ExistCachedQuery(SQL) {
		if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
			return 0, err
		}
		if len(rows) != 0 {
			if latestIdFromCache == rows[0].ID {
				break // no new rows to process
			}

			latestIdFromCache = rows[0].ID
			latestRuntimeFromCache = rows[0].NewRuntime
			SQL = fmt.Sprintf(SQLTxt, latestRuntimeFromCache, latestIdFromCache, networkHeightBegin, endTime, nodeId)
		} else {

			break // no results
		}
	}

	if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	timeLapse := time.Now().Unix() - startTime
	fmt.Printf("\tTimeLapse: %v s", timeLapse)

	return rows[0].NewRuntime, nil
}

// This is not very optimized
func (m *Metrics) recomputeRuntime_old(nodeId string, networkHeightBegin uint64) (int64, error) {

	const limit = 100
	offset := int64(0)
	totalNodeRuntime := int64(0)
	var nodeStartTime time.Time
	var nodeLastRestartTime time.Time
	var lastMetricTime time.Time
	var latestNodeData models.CelestiaNode

	for {

		var rows []models.CelestiaNode

		SQL := fmt.Sprintf(`
		SELECT * 
		FROM "celestia_nodes" 
		WHERE 
			"network_height" > %d 
			AND "node_id" = '%s' 
		ORDER BY "id" ASC
		LIMIT %d OFFSET %d`, networkHeightBegin, nodeId, limit, offset)
		if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
			return 0, err
		}
		if len(rows) == 0 {
			break // hit the final row
		}

		offset += int64(len(rows))
		for _, node := range rows {

			if nodeStartTime.IsZero() {
				nodeStartTime = node.StartTime
			}

			if nodeStartTime != node.StartTime {
				return 0, fmt.Errorf("start time change! expected %v got %v", nodeStartTime, node.StartTime)
			}

			if nodeLastRestartTime.IsZero() {
				nodeLastRestartTime = node.LastRestartTime
				if node.LastRestartTime.IsZero() {
					nodeLastRestartTime = node.StartTime
				}
			}

			if lastMetricTime.IsZero() || lastMetricTime.Before(node.CreatedAt) {
				metricsGap := node.CreatedAt.Unix() - lastMetricTime.Unix()
				if metricsGap < 100 { // The time when there is no metrics (heartbeat) we consider the node down (seconds)
					totalNodeRuntime += metricsGap
				}
				lastMetricTime = node.CreatedAt
			}

			latestNodeData = node
		}

	}

	// If the node has never been restarted
	if totalNodeRuntime == 0 {
		totalNodeRuntime = latestNodeData.CreatedAt.Unix() - nodeStartTime.Unix()
	}

	return totalNodeRuntime, nil
}

func nodeUptime(node models.CelestiaNode, totalRunTime uint64, networkHeight uint64, uptimeStartTime time.Time) float32 {

	totalSyncedBlocks := node.DasTotalSampledHeaders // full & light nodes
	if node.NodeType == receiver.BridgeNodeType {
		totalSyncedBlocks = node.Head
	}

	// for nodes that started late
	nodeStartTime := node.StartTime
	if nodeStartTime.After(uptimeStartTime) {
		nodeStartTime = uptimeStartTime
	}

	syncUptime := float64(totalSyncedBlocks) / float64(networkHeight)
	tsUptime := float64(totalRunTime) / time.Since(nodeStartTime).Seconds()

	// tools.PrintJson(node)
	// fmt.Printf("syncUptime: %v\n", syncUptime)
	// fmt.Printf("tsUptime: %v\n", tsUptime)

	if syncUptime < tsUptime || node.StartTime.IsZero() {
		return float32(100 * syncUptime)
	}
	return float32(100 * tsUptime)

	// ref: uptime = minimum(total_block_sampled_or_synced/network_head, total_node_uptime_in_seconds/(current_time-node_Start_time))
}

func (m *Metrics) GetLatestNodeData(nodeId string) (models.CelestiaNode, error) {

	var rows []models.CelestiaNode

	SQL := fmt.Sprintf(`
		SELECT * 
		FROM "celestia_nodes" 
		WHERE 
			"node_id" = '%s' 
		ORDER BY "id" DESC
		LIMIT 1`, nodeId)
	if err := database.Query(m.db, SQL, &rows); err != nil {
		return models.CelestiaNode{}, err
	}
	if len(rows) == 0 {
		return models.CelestiaNode{}, fmt.Errorf("node not found")
	}
	return rows[0], nil

}

func (m *Metrics) GetNodeDataByMetricTime(nodeId string, metricTime time.Time) (models.CelestiaNode, error) {

	var rows []models.CelestiaNode

	SQL := fmt.Sprintf(`
		SELECT *
		FROM "celestia_nodes" 
		WHERE 
			"node_id" = '%s'
			AND "created_at" >= '%v'
		ORDER BY "id" ASC
		LIMIT 1`, nodeId, metricTime)
	if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
		return models.CelestiaNode{}, err
	}
	if len(rows) == 0 {
		return models.CelestiaNode{}, fmt.Errorf("node not found")
	}
	return rows[0], nil

}

func (m *Metrics) getTheLatestNetworkHeight() (uint64, error) {

	var rows []models.CelestiaNode

	SQL := `
		SELECT 
			MAX("network_height") AS "network_height"
		FROM "celestia_nodes"`

	if err := database.Query(m.db, SQL, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("node not found")
	}
	return rows[0].NetworkHeight, nil
}

func (m *Metrics) getNetworkHeightAtTime(metricTime time.Time) (uint64, error) {

	var rows []models.CelestiaNode

	SQL := fmt.Sprintf(`
		SELECT 
			MAX("network_height") AS "network_height"
		FROM "celestia_nodes"
		WHERE "created_at" < '%v'`, metricTime)

	if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("node not found")
	}
	return rows[0].NetworkHeight, nil
}
