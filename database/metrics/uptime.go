package metrics

import (
	"fmt"
	"os"
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

	// rows := []string{}
	// SQL := `SELECT DISTINCT "node_id" from "celestia_nodes"`
	// if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
	// 	return nodesList, err
	// }
	rows := getPredefinedNodeIdsList()

	for i, nodeId := range rows {
		fmt.Printf("[ %d / %d ] nodeId: %v ", i+1, len(rows), nodeId)
		latestNodeData, err := m.GetNodeDataByMetricTime(nodeId, uptimeEndTime)
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

		newUptime := nodeUptime(latestNodeData, uint64(newRunTime), networkHeight, uptimeStartTime, uptimeEndTime)
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
			AND t1."created_at" < CAST('%s' AS TIMESTAMP)
			AND t1."node_id" = '%s'
		GROUP BY 
			t1."id", t1."node_id", t1."created_at" 
		ORDER BY 
			t1."id" ASC
		) AS subquery
		WHERE "time_gap_seconds" < 100`

	SQL := fmt.Sprintf(SQLTxt, latestRuntimeFromCache, latestIdFromCache, networkHeightBegin, endTime.Format("2006-01-02 15:04:05-07:00"), nodeId)
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
			SQL = fmt.Sprintf(SQLTxt, latestRuntimeFromCache, latestIdFromCache, networkHeightBegin, endTime.Format("2006-01-02 15:04:05-07:00"), nodeId)
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

func nodeUptime(node models.CelestiaNode, totalRunTime uint64, networkHeight uint64, uptimeStartTime, uptimeEndTime time.Time) float32 {

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
	tsUptime := float64(totalRunTime) / float64(uptimeEndTime.Unix()-nodeStartTime.Unix())

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
			AND "created_at" >= CAST('%s' AS TIMESTAMP)
		ORDER BY "id" ASC
		LIMIT 1`, nodeId, metricTime.Format("2006-01-02 15:04:05-07:00"))
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
		SELECT MAX("network_height") AS "network_height"
		FROM "celestia_nodes"
		WHERE "created_at" < CAST('%s' AS TIMESTAMP)`, metricTime.Format("2006-01-02 15:04:05-07:00"))

	if err := database.CachedQuery(m.db, SQL, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("node not found")
	}
	return rows[0].NetworkHeight, nil
}

// This list is extracted from the knack portal to make the calculation faster
func getPredefinedNodeIdsList() []string {
	nodesList := []string{
		"12D3KooWAbFAdwGBugn6TZscAMy6cde8n8DLoFJywpsLJLhWahFT",
		"12D3KooWAuemDk5eDa2wjY4LukoNkhH1ZN2DrVdLPb3SLCMVGRd9",
		"12D3KooWNNTn9zJDA6atuswEGKknFYDgzBkay6uWYPoWVBBid3DH",
		"12D3KooWHB32cdsPLemcuJdjyLUJdWqp1R4GJK6s2ueqJvfxobdg",
		"12D3KooWEui5pbMi6fvD3xMb4oVXs1zNaGAXePQwHKEqrF55FAZY",
		"12D3KooWSZ3gWkNbq14JbSrp4E1UABVAdkN54tb8fK62h9uQxZKE",
		"12D3KooWNCvVezCkyiUcMT6NL11Ue4A1RF7y7Xih6WyHNevqVYBC",
		"12D3KooWC2NnJKTnx3fd1d1rbVL5Kaa3U66L2XeKMxYjmamPTXut",
		"12D3KooWJfbcYAdTaXZEHQWem6fatJDUpoDKUyXw3HXLcBHM2BkV",
		"12D3KooWBqQN7ee9XiHAesHAmVzfvwkLv7UUyj5KYUZngRCSs6Ei",
		"12D3KooWP5toR8N1xGUZm6M4KzyY85ss9hwpHevdxb8nMJQBRUFv",
		"12D3KooWALp9MykzrkDEtSQRX5b3u7aVs4sK8kkEScohRu3N4GDb",
		"12D3KooWP8dHHxKzkVf8bdYsVEXfZKkA22votc2FZvUjESpYU1o1",
		"12D3KooWD13hWGqHXobDggdrFk8UPSBDRGHfheDzwECLsiK8HNFD",
		"12D3KooWQoNNzAStQvx4ADFHXJ9jXctBPFa3jbmXBMTCyYkF7TMS",
		"12D3KooWFu69RanHPBzSb8JWE8vNNvM1doiDoost7zQAtjCwEhV7",
		"12D3KooWR8nrvDwJotJuNepHLwQPp5LziNmVTT6J563B2KSb1Se1",
		"12D3KooWDQjXzsvXzrLBZXbJ3jxey58LLeMXEzjC34UDKmyZqkBr",
		"12D3KooWLx6Cmx3tvgzvP1pTwuDJVPYoc6uMNS12kUyoDXkyswwY",
		"12D3KooWMUbze6jmi8xaKgKzuZkD3NF7oJWRHSZETBub6a2r6cRj",
		"12D3KooWBbc9ixMAH4SzF3EH6SwSfo8gRW1bVrDUX19oxC4DaRbz",
		"12D3KooWJWFuueY4dKh4rL6kza7YpWzYt7v1ZiaudGPTKaF3nQYT",
		"12D3KooWKZ6wV2YnGdxvJPh8LsCzNmV9GGPqncgAgsRf5oatNuBF",
		"12D3KooWBCfAdTYhtpuzbKqnc5WM3qh7Zkp3DmjrbAwgTsgiKQPW",
		"12D3KooWSXtCGigwFp5ELGX1vKpW5GxmemKPLaRDpYdCZAGVeQFu",
		"12D3KooWJotWyNHUTV9SwfAza9fkLVpkxKBsyucHe7rL8n9X7zs2",
		"12D3KooWD9S5LPfPVnDPy25PDFGimeEQ38EPQmHgoh5u7yz8jNhH",
		"12D3KooWMXRFr4KbbrjZETfQPoSQUSjeV4YxEbGLCc37pEdZ2WB1",
		"12D3KooWS3egcQfrQdS2dQHtUCfyNGNbmowXmy95B3k8UDQPppZz",
		"12D3KooWPTNcGiknxFkWFSjuhFFtgxu85zpqwwF4uHGbqwkwdXLY",
		"12D3KooWR3BSihycpy5pPV2DjTYseqpRwF5BaLi9PzbKhxyy3mfU",
		"12D3KooWL7nPYu4fyABYg2XnHYUetHjj96cMcRHUjJvZHMkKiV4x",
		"12D3KooWQiZ7aE9jdZCZRqGsR6k4X7wR1xD4SMJQZFmFvZEBGuJL",
		"12D3KooWHDABgJKo5rb2AM22u5wcQqXLXZZ9uB4yiXfjpNZAf1EH",
		"12D3KooWDHmxrf5AJYVq28YfzAaq1yuMedE9avrGAbSp5JbKeWW9",
		"12D3KooWSRbJd3FQ32zBGqtLmifQdUmkHcJAhHzLKX8zmb8Ri5xL",
		"12D3KooWCzMPySynEksLdW8u7oqJxUtvwe8FkvqHNtgbQfatu1F4",
		"12D3KooWCdDehewhqwDDVHh8EFh4ugXQ5UGEPeyY9p5tKkSr98JM",
		"12D3KooWCmkFRsUipHAbdnEfTnB5CKAxdKGDMghriPv5MJsboRp8",
		"12D3KooW9ucxLJVTPeg62XUVBnKPcf2UZ9t73biujkczov5X63AV",
		"12D3KooWH7ZCwtXXnZ46PfiVuLhHwBcXEZVscZACUaLox4cM17Wg",
		"12D3KooWEkFUj5rrWoeQL9stJR6AkC2F1MBT4MUeiqaoi8ZUAADA",
		"12D3KooWGsSASv91r9kNeR1RPJgBAh8D2UTGVTRk6dPQrZbpFkrM",
		"12D3KooWFrcVNHxW1SmvjQaonrvtvEoZH9CfFou4Dj3tpcgZPKuo",
		"12D3KooWP4t8E6Nvg6FUTAkZVw6hn7x1BYH1TETrtUvJb4NWc2Eh",
		"12D3KooWJqhvFkPKNHwmdbaVCVEM2ReNhTWCvStLGKqvFKWjzkHv",
		"12D3KooWAx4iNKM2x2caCKcZJmpcc49P9BN38yUPqQz1ZDygrmpV",
		"12D3KooWRmzCosNSVeeZnwXwm9JSmcq8CkhMYAG6J2PUscpuK7t2",
		"12D3KooWKwPLpmiTm7F3qmcnV85Yue7jVj5uT6uupMUrcF9NJ5UD",
		"12D3KooWAMKhsAMRy8XX74BC1hwL5hS7u6Zq62v9bZ5sYnRQUrao",
		"12D3KooWSX95wRawq7xodw8VuiE3rhmWDqwcoTzev6xPMH2NWGxF",
		"12D3KooWLmYsuABAF8AKXHz9mNfnNqQXQve773C2PC2BjHkQBm3R",
		"12D3KooWFhksMiPgjghVa95JEHmJbLTRudrftvWnd1aCRWfv9WyC",
		"12D3KooWJjjsPqL6wAgdHFkxd1CvPcMCoVPrWhKVGQcTdvVrkxdK",
		"12D3KooWKCUqWubL4b7c448Kkfdem9ZpZ33QnCHzub8Mf6bQZfFs",
		"12D3KooWFKiR9qk2P9f2bkHUKCjdNUvexSUKU6eacQPzG3aCfJf2",
		"12D3KooWPTBzFDWrkic9jtzWdZAMAvGAynXMnpgZsFz2MeWpaQBA",
		"12D3KooWLezAd29AtXb1GX8VumbjRGc4dLnr7knoaeejBUtQcSVA",
		"12D3KooWMUZfzaeRPQZaCSrFm6TLV9G45JNhWhDYSnQ2KUkFQ8wH",
		"12D3KooWCshF2rNvcMJREScaP69JtHjSGY4aySpXcpbtuSgAGWC8",
		"12D3KooWKizfmnhXq1NBBkGNa3mJMqY3VPGGjQ62jCzRrMVTg9wv",
		"12D3KooWL3pc1XRapg1LBzHzsA2vYNRiAoHVjQujW1h4DePhvNGw",
		"12D3KooWRWvRwYLwS33dvCW55xGoG9J4acbkjqPy1K5k8kq6vp1Y",
		"12D3KooWFC3v38kFFJYWVNjR92iX962YpSVpAbGsDwYcgVqvTWk8",
		"12D3KooWJuduMfQ2QUNhaJyrfc6iqfv5DS2Z2v4LNU5ji3yVL9hH",
		"12D3KooWQpubiFH2MFPYj2YvfqeV2r2wvcMHYC3W4BUpDRTetgnh",
		"12D3KooWEF1cTm8eCevybhxicYWDuYWHsa2YgpV3VeS667XhFcK9",
		"12D3KooWHWX2xBnsNbyF9HoHdQFtBhyPh9xzGiWWJe8dyCUXSWnr",
		"12D3KooWAM4QADEHeZGttANbzKs2wDAWPa8LrGhcc1HjoE4my8uT",
		"12D3KooWT2ytFuy41soAuW4WV5tRTwaoUrXtGesSyYEjjet1wDxA",
		"12D3KooWSDg9Fdtw658TuAGd3khJSaA5jRCSF468oW4f3XD7k96B",
		"12D3KooWDFcV5ZLojGqtkRyv53PhSaBCes7eA18ZCVjeGBYk1YGw",
		"12D3KooWBaDuFJ3jpiccYh1nkf1Pwu6f8qGB82aa6dmc4WzZENzU",
		"12D3KooWHtJYVcuwwpdgzzJhunDiDVrqzPRzjcvHj3F1bRMp7S9y",
		"12D3KooWE8eZY8zvsLRdnsSj2jgezL6kFxNqc8vYY7mRSUkYXKqU",
		"12D3KooWNQgu9ZKb2DiU5hdo5sMxJMg8dPWp5u7U4DdEpv2tSiPX",

		"12D3KooWCLSWy5Z1fyCR81odHJim6YeTaLoHLyMKVX9F5ARDsyZb",

		"12D3KooWRCkUWY6F633e9z6RZarp9pqZwzkXzZefnve3nhW4ZvWi",
		"12D3KooWEFkCqWpLqtQzXkAxQGYoxaCNv4nh1P5VNmNa6TwSHL2b",
		"12D3KooWShTv6HkQEFg5PfkKr8E2mZXtGqh7nUSzr2GcpUSehnFq",

		"12D3KooWHX5emqECbWkFwgnmzSEgpktcsnpZ5mFqGU8bmswCdV8h",

		"12D3KooWFJo8z2aF67rc4aAdHyn2WKQqUiPh68HYon8QissuqwBE",
		"12D3KooWMDhYRFpArbtsNGCaZaDBfgyMfNM1RR1Zdo66XkJNsWyn",
		"12D3KooWFTRiL1BGybHHN2vGvUtDgzMqEqebcNCi1nZqiuiB9CVE",
		"12D3KooWEteLFe3RB4qhPT2iLZrnSF9PBbqfm3RxneR2u5dhwGrm",
		"12D3KooWB2Xgy2LbVDX8TYR3ns2UTitbKE8ePzoG3jDs3Vb723sr",
		"12D3KooWDJ9zp8cnoDEH7aGxuSms8yQedtJcDYrKq9VUcvwZ5nTU",
		"12D3KooWSARwXAquPN2ucQBfnEYBChrnGgtNjUHKvH3BvU6PxzSs",

		"12D3KooWQM4pDj59T1wTvgSYW6CM1F2BDPxvunmjybuKj4NqkrnY",
		"12D3KooWMRx5Kx1ns5228wDumUK236FDMgdpuhNnu9mdNhC196P5",
		"12D3KooWHS88wVAnaRpLEXRfjkJqfmuVg6iRqYDiQ7uth9cxUp2X",
		"12D3KooWJgZAWDE1Zm7ewqRqm2qrGM7U5uTcqkUU6HNdBZ8VoyKh",
		"12D3KooW9thhJz1ThXkwc62xRFhsNtsrnV6JieosxhUDygGEiU7G",
		"12D3KooWMzKkW67Ycy9pNeDzhGb6VP8wQNQsitw85p8cZJg99mPU",
		"12D3KooWNtyGzsfueXJpYwHLqnRdjZGV51Rw2XtBzj9PXxWWeKGg",
		"12D3KooWMk8Tt3Yp2LL1tgstTdgRmBL7iEEzVYz9oiSaW5EDSEoG",
		"12D3KooWAfp9vkBDTy8LVvpJLMJbAi9dBkCqXGgFWG3oZvhs7kYR",

		"12D3KooWKiFnpWhYZCXwX7HLNXQNpGtrBnuYmT9gJkHJs22rNRPn",
		"12D3KooWMjrSNHSvSmEUMv3obfWwV4bxUk86CtCML51ewNPaEdqp",
		"12D3KooWQvht4j2aYVrALb8mNwRs3hEmEbhvQaP2D5puwtEGe3jB",
		"12D3KooWBpZE5nvFAJSyE6ZkHh9Zz9avVauoLfc7vw8KgBYhBHpL",
		"12D3KooWGNJqGAxP7XxVG3qY4n23NZ42Hz4iv9mf3",
		"12D3KooWGNJW1ZEYEeFcXVfw1soJifLVTrM6DSiWhPR5VP9ntqg9",
		"12D3KooWDhaRPhpg1nLCshNZXELD22zmxgXCXgoyL4uLmZq8scQJ",
		"12D3KooWJBx27WJCYTFCLVgp3NSQJ5SoHxikygjwsqSu8PJWU2aU",
		"12D3KooWMZmc7VCCC3bzfqZqGM21zZ6K48VTQb5SX5hfNBroKDwi",
		"12D3KooWJbpjrBUu2PP96QUqm9rAzDasLvfKLXGQZNPZZrZe5VyE",
		"12D3KooWNsTQo4WHqGMkupHvNL1Fa4kVUMVkzRzCBqiopAfAmcbC",
		"12D3KooWEbdhKJXggTiezYC2nyZ6PYx9qfkX3muKUCM3SKvrybec",
		"12D3KooWCBcb6SLb8hrpYrxXVT4zhv6Mf7sDxFbarqiJMr1XRxMf",
		"12D3KooWAqttCypqrRpSKphzaB77KimYJu5X68u7zm5MsJwddhiH",
		"12D3KooWJou3AcyA3vovdaFVU79cTnGAhBzRH4rdY6rpMhxx9vpY",
		"12D3KooWEsesEBTpXNPnNxH7QorDAfPbKc2pdtQwcwHBab89bLUS",
		"12D3KooWAEWUfwtFxjLCVhVmonkQwwNxbBSFsb7VGfR4rFEfTpUW",
		"12D3KooWMYqt6nJ9ew4h99zRjue3jmcHHyFXxDeyYkzwNhX8Nkvs",
		"12D3KooWBZp2MhRS9v96MNxjmWDmFoszESjYAxgHT3VRMSWL9QUe",
		"12D3KooWRdoj2wiqhdHX9qP5EHjzMpUcYqxnrrJKu1cumTTbZxnQ",
		"12D3KooWAktyreXmqQN6iLr1ausLgSJFU7rSdbT9vMXemku8W9GW",
		"12D3KooWD1oyJtzc3dhfUK2DZ1SJzPwLson92fnGBckiXpGixxBg",
		"12D3KooWRxb7kkyLc9Nxm5PQbdMxZ96vdaqLJTpHEKiPH35Ei6vj",
		"12D3KooWGhV9EZRGFHMLnEkHBG8yQcM36NPYsp6vY9aN67vKpXr6",
		"12D3KooWH5A3bmhqCdKikCPBKj9h9gyA5vasGwghYitensAcwgZV",
		"12D3KooWAdAJe4e2GLWCrALkmeW4cVrVTdmmmqzhuurdsSqdM61G",
		"12D3KooWPe6vwwK8zUvgML5x57Jigmh7ZqAdzuiCn3fCtJFzWQLG",
		"12D3KooWFjSCP1ekFA8g2rFNsxGLSAk1TJUPfkNWGypjY2LBZqn3",
		"12D3KooWBtycSd8cWST8uLPt76qGQkqsL2W8bh1ux5b87fatnj29",
		"12D3KooWQfXcqT1225zX1ZC356JrkX88UFZKYADxDxGEAiySmEd5",
		"12D3KooWESi3Mv3V39o8vbLavdmQStrEWJpGzVUZPVV3VajVP2tw",
		"12D3KooWDMA5SmGxqw51i7FZyddnDTDYXzdYJZNtyTRwuk5kTioT",
		"12D3KooWRLsPN2mQS4crUkCNcD3CYPjAN5mjreoK1ZjkHALHPTFp",
		"12D3KooWBKdncfbf3MegmmLrVCaB4BjtHihmFrKPSAxCLWesrsVF",
		"12D3KooWATN5sMaiJf1tEzidn8hU1nNz4PVbQL2FPUYQUQN9qPKK",
		"12D3KooWMqrx3cekSyR3bnNioiSYkSaPiRrfKF3UAYDzYrWD5SfE",
		"12D3KooWEFEBUkabn4isPDxvywtBZZ9ov11Z8gAdfpKGgoxJX7Lx",
		"12D3KooWPRCKxVhifN1WHRqW56YW2J16nvdCsZqTKsPZKqyqujWP",
		"12D3KooWBKq5L2vVDzxpiYE77f6j7QnbiXTDNtN68WyHDV8T1nSH",
		"12D3KooWFKNTDwLrbbZcdouc4qY32oCbw6BfXMJXaT3fUJDyD9N4",
		"12D3KooWHDYWSp1feESgPHnEorCpnRL9D7VenGBYKETcFa6HNkKm",
		"12D3KooWJagjCr6p8pq97acEpau5KXbBt99cpDYwAQs5L13P1uxx",
		"12D3KooWJ3Uv99RmPZq939BREwVc3GGS9bvrMjzvjJ7ZoNr2AU1y",

		"12D3KooWBTFMz9doATz6gxb9VUZ5d2iJ1udBfJCkho6ixgeu6nhE",
		"12D3KooWG46QmeWUWAwPuyoUnaBkMMjA8WpM6w2d2Bpimgh8U2p9",
		"12D3KooWHzJhvkgsANyA3ipac14j5RhPvfTy4x1m6KKymht1ChvJ",
		"12D3KooWM19jo2TqK5J1dnUdkEFu24HF8RjgqctFerhpZdjkgaAK",
		"12D3KooWMbtiwpvcfBAwNcBubFNpCu3ajU8tQzZ3SkmxFtLrtQwJ",

		"12D3KooWPbePdhXVre2EASCZg32K7C1DAgbVcW2xkBQGYAvBbAJU",
		"12D3KooWCcc37UYhb3pQzJmeoYGXh8Vf3PxGpffiE1g4ynWNdpro",

		"12D3KooWH4zt5UtqzLccG7cS5HKt3vhAGWfZbN33dnjFe1ke831F",
		"12D3KooWEyP5VCR1iob3UuLztCneM19fMTnvLrUQMu4LJVuHCZbU",
		"12D3KooWLxCCAabYGfFiPikroNfjrJRNPtN31uMDF6mSu8QNv96j",
		"12D3KooWGHqdPFz6e3D5Miwh5vrDv5TvF4JQW6iiYUA8VibJQCpF",
		"12D3KooWBCVnK6FeKXk33kzZZFwLrX5irRoPCp7Drci4pBz2M1nT",
		"12D3KooWMLPKhuHBbPCi4XjSWS31uQ2YGyTDjkLpLCHD2GCNQ47f",
		"12D3KooWLjsUq1dpE2yTJQRTYYcuYYJSHX9EfE52WMiQvD3Vqu69",
		"12D3KooWBG3idR4UYYrEeDXbot99B9ZxRdzdNa7sE7hz3JuY2HuZ",
		"12D3KooWDwnHvWQU6CZByaho2MEq1zeB9AevFigkhR4nVF6fVVZg",
		"12D3KooWC68HRUxmrG5ugwGrExwhupBQrpYYzL6AcXQ8rCPca74K",
		"12D3KooWMnmvFLFxPhLSZY42nZHQ3VrYa5vzhSB86nwRzXRnmm26",
		"12D3KooWF5LSp21QUEMJjfMy7ji76bjp9BsMPqrz76BACxwnSJYJ",
		"12D3KooWEmrxY6zNAMn8kfi9RXwc59tftFKnKsiHWZZrS4NFWu13",
		"12D3KooWKV5QF9CkkbX4aYP7bFT9Rga9XsoaT9aAPivdgUPqzmKN",
		"12D3KooWKZNfKRZoy6sikc4EMQipuSCFqdQoWwyWZE3VLuJd14Pe",
		"12D3KooWH5WZy8a5QCNHjLr7VPHJ8TjfQURjVpdRWUVccRCm7U3q",
		"12D3KooWBTXFg1P8nRcMHL3xJfmgc6EeF7HtaU7WQJdJ1eBBhj6y",
		"12D3KooWC6Vg18RW56jjhmCNQEtWg6iyAmPyiy3jcC2WPKKiCyKZ",
		"12D3KooWKVZHt4DDmRfQ3tCd7rg1H7uXrYhc27ob46oga2hQiPfH",
		"12D3KooWFvZxuvPibmMKEW2WHGCzZBmPfa3AefKDvUtjWvsAcadz",
		"12D3KooW9pg4EjVkz6piVpV6PyjrQzJLfpbc1Yh1hW6EH32F5rLk",
		"12D3KooWLrpuxkVxbfP1Pj1raz7S2i4K9eyzpBVoHqsQAvncW5jU",
		"12D3KooWFqhvZeBvSLKcX1fjy6BRrBDfKhgLnVqD1fRoLZeZ48eM",
		"12D3KooWMhFq1J9u2xgNi2JNb5FvT8E2yUGC7f62Hyj24c6nMNfp",
		"12D3KooWJMeP2LpUrQp1UsAa14WozoACkeCMDBLzEJy7wD56JsNj",
		"12D3KooWJcxCqMC8fEowFBZqQfgj58tVW8tvBXghfE4TgfxMA5MZ",
		"12D3KooWE4ipTh6RpXGy4fMYkkpgJz4ckFDJBKYVJZPFMGk1KkX1",
		"12D3KooWBaMEiW12SosTPs6ucaqxCfddN23dvhFD1G7ydz7E8ofb",
		"12D3KooWN4bjqPXdBN3HNwtmZRv5XS6oU662xQyiKpDFwbhHcAZq",
		"12D3KooWEPVgK8QXAhcYAFhxxjnbDbkCvN2MkRNueiNxc7VXanvh",
		"12D3KooWP4h83oXh8n1QMbgvXsb6vpTXNaXhX6mbCMeiuGyUBdni",
		"12D3KooWPvtZ6DfVLuJSLMU2mY9r3aNAMwbUiDJ7uScrpBW85yqk",
		"12D3KooWAUbSkPUx3yAPH2z6CGwF62caYDNXJZWugb29JJApuEE6",
		"12D3KooWC1CHQDLmPBDxsqSfGd53PTnbirH1f5w1xKzvy7WENeCi",
		"12D3KooWNreDwjwXjZJQhYJvJC5bLpj8XF5VpCqV25WyprsZf6z3",
		"12D3KooWSpmuDUXYz1oKj4pj3zVU2UtfmCn8AqyeW4xfXBqJqGLD",
		"12D3KooWJ4JV31YFSgyoUambEsv4vPUFpZLyTe7DTNUmnkwN9gDi",
		"12D3KooWHNnw6J6Mx1UGVGxdVgGGzGL5YjJUvRhFTPBYR3FH8Fmo",
		"12D3KooWLqVGZWmfBzaVXmYbATjnZvcTphoW6yXSt3TqVdredNZo",
		"12D3KooWR71aNbjXQLehMmQQpLPYpwhnTZtDwjRAx3niiLFz3A3z",

		"12D3KooWSJyRiJcKgb16Yrboh34E8WonymjRiynUoSc2D8JfsFk2",
		"12D3KooWRovC2HRoeyV5T1Jr9YsPq21BHnkXSe7VvKoZhfd53fbq",
		"12D3KooWFXoMFqpiJ9ZHQiDrzD1maGQUGaoG2gCTZAfu3G7",
		"12D3KooWKGXnjpiRPv9pcmMb6y4wYe4EvAXv3M3JrFggNoHwa6da",
		"12D3KooWNy5QHnpkpthWCJPUGu2rKKhhVq72uXuWpJShL5wo3Fgu",
		"12D3KooWKxBNNzwszyePwvxw3nwDs3oaKwu2FXAecPygRXAJ9Q2T",
		"12D3KooWFiZZoUp9wGSqqpSTryXHQtJZpnBVtfHKbuZMFHLZZdEV",
		"12D3KooWMdm1GeK1J4ru9PVeSnFBCyme2J1fELSBSFsEKkiFAngN",
		"12D3KooWPoVpPuErM4mKpNsE3g13cGDSbPNa6ME69as16W9fK1G3",
		"12D3KooWGpfsVPwk6prN9H9a2msAFTrdqYozGGCaRzxWR9kS2rsF",
		"12D3KooWEMCo7QkLJsuHGPLXZxNZpMp1GKQFXBZMid68b6PkuRwp",
		"12D3KooWFPyYWQGgzfSemF1DH3FGAzL5uvNyBKjCEHeFKZFL1Qbf",
		"12D3KooWFNydpwNfTcWQEfhUg3ADR1gk5ckqNrgQCpPbYPqgGm1D",
		"12D3KooWRP4hxqrANnXiqCjsGokPk7dswwAHPFSD7xcWh292gkvL",
		"12D3KooWMtEhk99xnHFWgNZEkbJmnakpBWNcQGw8aCcmQRa3FhPb",
		"12D3KooWNaJgjBXvoHRBtc8x6EHEsF4A3jtH5ei3M3m1F2NYWuhk",
		"12D3KooWStMYiZp4VBstfCrL6yt531tAjgT8AbumaeLn3T6SHHpL",
		"12D3KooWFmtni52wAvjKJ6QbHDwBYqp2M5z7ErHwNfT6hRpcER6w",
		"12D3KooWQ1UCgcTM97p8uePkr362Ax54MCU55NA6h2g7Dp4tXihW",
		"12D3KooWDFNckz9BLP65xQCg52oas7MyPaBKDS3mhChisJVGr4TQ",
		"12D3KooW9wC4i68yszaXbaDZLAxbjocTFD25e9ysv2qYBrgtQjAr",
		"12D3KooWDbczTMaFhXVutPMRj3MfTf3ZTjcfzLgU8ohbzbJSDWh8",
		"12D3KooWH5V6WqzA9YqHznBVw29B4YHNzuiaQFo5UKnGxymFXjtN",
		"12D3KooWBHko38kfhuwU3sZGjeBYSYQt3A7HY7mA9nG1Ynf5BG2Z",
		"12D3KooWANULEgaZb8F2q321jDFzqfFg9JoKY3SjkrrS1DtmHDDE",
		"12D3KooWMuVn3EU9WB8dwrArEtqYNgo4uxFGtquwt61Twbyd7hkF",
		"12D3KooWA4VcP373Zc4bGzUj8jaBZSbcHJ54kdrDdRGJYZrykizv",
		"12D3KooWHgHNVxPcamvYMAeyLvNJb5RNtYE9xbuQMT8kTSAM6KQi",
		"12D3KooWPKnqMrJDcVqR83g6KCsEEKQYoVmRo24Bc7u1j98HqQe7",
		"12D3KooWE8KJEsbwdDGpg5KxW1LtyZSEZaEueYUNxysoZ8n949d9",
		"12D3KooWDtmgia6UVJFSWg5enEJ4EVdR6qbwyAVmnieXnNppjnmp",

		"12D3KooW9zUKveJZAL1kRVoSKxFXjJJJ77Gp89VveyPLDpTnvXAP",
		"12D3KooWKW8Ufrv6GhQckVJXy3LNMsj43JgVRBZ57o8Ma4rPQjs9",
		"12D3KooWLLQ3jVE26R2Tzn2pG1wdJoGhdoUFkiaWtj3BMBVbBHr2",
		"12D3KooWFNPkdnk2mHoZeecJ41L6KUqrqsQZdKgc7NyHtBMLfTvd",
		"12D3KooWJcU3gKwCZHpMG9tdLBPT4LhZg5BcxNCzJPveQEYbwUfp",
		"12D3KooWCiCcLgWmG9MB8bbcwjiKHF1Htj1akJMmBZvNDs1ujMSS",
		"12D3KooWPW9kNxmxxzZ7b4GnAywX8ddcXPoi77n3xxMRtub3sfRd",
		"12D3KooWHxuKfuXq6hseL8nAG7xqe2zcajs7aXQykgThtxHnNM3Y",
		"12D3KooWLXwu6pqghyQ8w5sYGJdERmKP4NPDRupFYvkZGw1dGNDX",
		"12D3KooWNgUeK9S2kDfuDaTi6JsGdMvH7TCumZCQrBafJwNvyPy6",
		"12D3KooWK7h6Bw4dQffuMi4EiHAsBQzXKNf2UiweTx4eDg5wLG7i",
		"12D3KooWPtNynhYLvKEn9ezmavd6r2sXMYsGfymuy51X5Ar3c1rW",

		"12D3KooWMbCjQo74y765WrWZ1FSk52bFenZcYbrJxN2EV1yERY2L",
		"12D3KooWBH2NMipyTsXrxAU2PwyzYM5HYsx1bcApYUHT5WqJsxH7",
		"12D3KooWC7wGQHVzzbGhxVMsDgbCoDSTXYKtiHDbuDVgTsqhXnsL",
		"12D3KooWNkwiS2FXNyyxYBK6UQe69jLVyTZYYWmKGgKn1aXARNjd",
		"12D3KooWSjZX8a7B1JZ58GVzfF64Znv7cQAfNyCgmJCyKiStpCZs",

		"12D3KooWLr6vFGtyL1R5SHPDhB7vsEaTtCS5rgNLWyLCWw3dN8SN",
		"12D3KooWFQFaoXNdXjZn1xDgSGCGiK9HMSTaxGaA7j4sm1ZKwqT2",
		"12D3KooWGm2vMqWcJDZsUgJyLg6vWCrXyRG58jzMu7BoXhtMtPFa",
		"12D3KooWRE3cQCRmnpD5BeLEhC34MMSRkzy43djT4vt7uGppgWQi",
		"12D3KooWBsBzSgwKeRtKfRhSHx3XAazTsbAnvNdchhNkTHudVNCx",
		"12D3KooWQVK6fV3csqYMMMRjxYtFytii81k46kMSivmYqzNkAxB2",
		"12D3KooWLfY4TXeMULAfhoofgQXreaQYqipYDw7eUn7X9npb15jT",
		"12D3KooWSSJjT1feojSWSCbC146SbhAmuzmozyPydZseu1GNZ9To",
		"12D3KooWNJZyWeCsrKxKrxsNM1RVL2Edp77svvt7Cosa63TggC9m",
		"12D3KooWMGiF6esKLymKHaYRoqJRa3RAT7QBUaSHR1WQpfcXVMtY",
		"12D3KooWL6dyz1SzAT4aXNfrUpnRVNiTFNAEzWkLP635auSEyeYq",
		"12D3KooWDmv9GW886tVRUSzFetZQTFhU2xw2pkGTwxMPxUwui3ZR",
		"12D3KooWMRKgh9j4BqHMG2jvTsPspmPR78KyFjx3dvxQqk9LwJEd",
		"12D3KooWP1k8aBrPepEM6RRzvxqaKVBNLB5GztQEFw3F5XLx9NND",
		"12D3KooWBuJjo5krJqg9Qs599dgdcQEeGmHeJCB3u7Dn8ug98HWe",
		"12D3KooWFgaLiVnFMiJ1xu8PXVGMYyMTwrGfZBYYRMShzJbMg98c",
		"12D3KooWM3NaFo7UFmz2FG92EAsP5avjKXyJoychhcuZMah1EDEf",
		"12D3KooWLgyKVooFCni3B7JjrZ1WmAggZswMwDF5VsjEc4KKSvdq",
		"12D3KooWPvAtjBUNMDZVSm9wnZiw39fYqSAaYdvETCueEAisbPQN",
		"12D3KooWFpiayxfw7M831TuNX3nb32Xf12WDZAd33zkxK9PeMXTr",
		"12D3KooWM6DUtiggHypFZsnXEvm5jCtnP5YkkEtoNB2pSEgL41n3",
		"12D3KooWSUvTzrB2yfy7Ar9EL9cyKm5ZY6dcEufcAG3WT1UVuBUr",
		"12D3KooWErdmto1FunA3kp7hzAVYH41syZdXXGSfAbaEgorU4PXq",
		"12D3KooWK181LnruKTGmkjanb1rwHZhpVGuViCBG79yhrLPa8JFh",
		"12D3KooWAaVUNtCNDHc2Lo76hYEgpB87NXtPFYFuZg1R7Z3kos9B",
		"12D3KooWNFudMf8xswJ6ZCKQAajkkHVm2tHk6iFCPeU84fZENPV5",

		"12D3KooWGhoF7XzZjfpv83yTBFnna9kPvChtuKnjAL7LLxMPChAc",
		"12D3KooWPdQdDLEFFpHsHCS15iHZFFg2cQ65RmVnePCjwgpZvsyT",

		"12D3KooWT32YHy2cRi3E6BaJyeGEiZ5zLXAk8WmjHMTYjFrJeFRA",
		"12D3KooWQgNkX3ZLUtbskr59spzz7P5cQDVwPshYpQccbviq8Nkw",
		"12D3KooWAd4RZXMKXeXn78fZfLRj3m8ozn5m5JJUhK71BWqE94Lx",
		"12D3KooWNNF1rGmWjuG1Rw5R5oxp9UPqNLRPzXhGuMvmstgTLSEJ",
		"12D3KooWHWpbMBDSZhrahaWu4Y6qggYTPJh9fSyqg66zQvgh5vgj",
		"12D3KooWBQp2uBb9yS9FacdxDA2R6LGN1uou39NFEWLnJXidaEEb",
		"12D3KooWHmSw8SM753Yexwzb66WSJMo8mjEDnCSqfayzh7HZm5rZ",
		"12D3KooWS7m25pQLakxVs7msZ7VgpP3XhXEVtJRZ2gZ1DxpU1eLE",

		"12D3KooWQhpaHZyxjMSErYxVT2oE2EJktLDTuVdk5wZQx6FVbfwp",
		"12D3KooWExNHZmQm4WdyVqu1uLDA7D5xtLSZ8SPsmhcuqkRXcRF3",
		"12D3KooWGqoC2DsqiorEG7dceLzT4ihKzHLguCo6EkLtrJYiF6tm",
		"12D3KooWSa9GqE5Qxt5GaxzCHxNWYcShdAx1gkHviZuwbUePca3p",
		"12D3KooWHP7jrKeynXa3z11LyTMe6uTaRBfHjjuDSG9qDyjBHfpQ",
		"12D3KooWCz5NPF33vTbmwSiXPoeJ4W6WxqDK4J3QVdtjTFY1DTbT",
		"12D3KooWKHC7fFYcvYDyNrjMkUbDe41KUB6fqkwHGRedf57ZYySe",
		"12D3KooWCdo1Ebe1k3HN9PHEma8DN1xqZGAgtpzeKWGoZtKfMpUq",
		"12D3KooWBodnP7Sm4QvnqCeUAgbZwkpxan35vpkxPk2ojeotSbkS",
		"12D3KooWJ62F1iA8GcCi8biVvs9asWDyappFJYpbrDsttcEfrTDg",
		"12D3KooWJku9AwoKHEJGtKqXe7QhQZsjBFs9sViQY2jgSWdehjV2",
		"12D3KooWAhFQEshPcgQpgW2Lp6wTrrvraUyuZq37Q5Gt5pkumUuz",
		"12D3KooWAKNBrzzGgz2gutYxyEG8gSjMg6kZ3p43Z539qv7LxDNh",
		"12D3KooWQ1jXtQQUddEk6Wm7MHhy48X8myHjM9FnxDJWGS8Bn7Dg",
		"12D3KooWHvfXFoGg8yd9ezLfNKyJDZnx1UDuKq7H1pVzkU7rUjT5",
		"12D3KooWEvAG3e4vDCVAmN3sqQ4MdZbhvoSmsV2HXHPGqDQzrG57",
		"12D3KooWHvHhTFuygkHCcHUQiJUmBLX2JC9hgJKGQowUdn9b4Doh",

		"12D3KooWMSpfFBbhkcBq3nHmRHQj8B9AcgSenvvVcMr9uhECzgQF",
		"12D3KooWCRVKjjXKs5gm1YtA4PsfFpBhvhKSTw8WGx5Yj8q5qiro",

		"12D3KooWMTMwWoXW1HLWNNnS2wzGMYJ21hXLCgjS7TRFy1sYBvgo",
		"12D3KooWJmKtGuYZ2SyeS7o4BRrnkAEGKMkSSy7mFVhn5f5DVJyq",
		"12D3KooWC9FvoY6deEYS9PtBbKxvC1g5W89BwN2NvfpuN4wmBZMH",
		"12D3KooWGusJtASnrLRiJXrQDnLT223yrf8BA6xen2Eq3BprPEFc",
		"12D3KooWNyGZgztTmaXNKG7JVzpuvPCvA8hES5uPn9WFd56izEzh",
		"12D3KooWRuRAwwvDst7VKBtRWFwJUriEUx3Pb3c66t3jyw3Fno7g",
		"12D3KooWJW4AzpfTnQYJDfKzhcU2UBfjC1yRz4bAK18mFLoAtcZV",

		"12D3KooWMqCHRGddphD8VExBBkB2N7hUenxn3kgR13a8WqzdyPyw",
		"12D3KooWNML73KwmzF71zHc3VjdhrwLoA3PexC63t8Kbc1ekJ7RN",
		"12D3KooWFSkmmBbFzdzCgKTCYxMs8txaouhtVNSvUuLV7Mdh2xHF",

		"12D3KooWH4kk3WiNXYdJkypLh12AG46HNmkrZNuDHLtRwqwjtGfN",
		"12D3KooWB2kJLir5kwLTnnssf3G1z26aE9nJ8qMkQzotjE7f453X",
		"12D3KooWNhf4yTspkRiU4eBbVVfTENeWqqmmkkBeg1cWvX6KnrAX",
		"12D3KooWFmcUvvYuwxtqqxd6qWTsewvwT6ktLiBT4m2LEEW1zRsb",
		"12D3KooWH8vu7p5vrJrNvr2vEut6K9x9j9QG71abCHdREd85J2GE",
		"12D3KooWKT3vfkYTujUDcpg3hJRnxTeBpJo1CMa3f5WL6EZpsYr9",
		"12D3KooWHHrkt8XFKpK8ZggBAzs8CBqFcsGYzcc1ESxorjUapJoQ",
		"12D3KooWJ97MGYhZfsaEZW2xf6gayzBvAwhiWhbDffwLfu5wqFyR",
		"12D3KooWJPgNXHGmcDoGGo3BqawmBuWByUW2uP2HziFAc4tM9meb",
		"12D3KooWPqcvqxQ8mubjREcLPTonckKy65o32agpS49fAH1ncxwo",
		"12D3KooWKJCMqXBvYhpyYaxNvgVeHmxTzBe3Meu3491DAuCWRGQL",
		"12D3KooWSmmWMVVdyv4fD1aGX51dZYwKxBsCXz7f8Cr7LAHjMLNr",
		"12D3KooWAujqgSpZdN4HwyR6AyMLcnfiVKYL3Prjdf62owzpTsqx",
		"12D3KooWCHnPqZqgJqeUydvLs54aUhBPtQobdf2TpJHKQMj4aRu7",
		"12D3KooWMqN5YdunKJSj5WMXWDaoHjhLQ6t65tcw9m4TzMq4P6Ad",
		"12D3KooWEVGiS4ZpcYcgpegXj9Qr4wsf2aTPEtadTABeSgGFvfoN",
		"12D3KooWGMx7VZ9tgYAk3GgAcK5p8NE5Y5VgUDTNvB6PY76BHgC4",
		"12D3KooWCuGrz2d352CRAf9tRnTRv2TkqPQvfL83f9xWJAVjLirj",
		"12D3KooWL3GBG4R8JMZntDDSrV5FPgCx4CJKpB9p9VyMwQexCFst",
		"12D3KooWRcMHuQseJ8D39kMM3YRiauvYNqDTxFu7ezZe7rEfD4H9",
		"12D3KooWDokfNbToEq5K49cv3WkrfZDQto1gna2KWWo4Ubivy91o",

		"12D3KooWGtaPSyCKLfASHUDsw3LREwZZJZaJ9KrLXFLnE9GbsGc5",
		"12D3KooWJyDzGabcVqD2arWGrkYkLEtdtFJYXBU9Gjd5ybMBMmGU",
		"12D3KooWRmj9BvW85SvatNC3Kf5VMWbpHJRDCrr1MK8Bmc4JV3aj",
		"12D3KooWJLpE63p3Hau36LfcoAutPLAgtnTxHa2HS74H2DR3oTzd",
		"12D3KooWQGsNDNe3qGdAQ8cxXeHZwftT4jMj6fZUUbgPJidJ4tfp",
		"12D3KooWJQrUUJbtz9tYvoJSne6jC6AwQYuxDzw5EsXLvR5M6rGN",
		"12D3KooWDbsmTs9C2Vk7NT6K7ZpYXMdbovStiYYxmYXYMVFkBnX7",
		"12D3KooWET2JuU7UvTRQHUousasD1ATVfFzdqAHLnC21tfc8ozfw",
		"12D3KooWLuHbVr8JE4hRk2cvj5xCxB2YmyRATdtJrcc4TmdNK92i",
		"12D3KooWEvGQbiiBWcZv71FS5kwkh5g5ZtYJXQYoDgNM23w3jmam",
		"12D3KooWPzTiJy7JtTg6zfKRyGMPahNML3AZuhJnXHLxLf9S1ogn",
		"12D3KooWR6jD6ry4TaXrNTZJ4mY4vgJskF4tYia8eScUNHwDGaS5",
		"12D3KooWCT9aCgwDVt8D5UGiiDhsJHNhbuif1utdxRBgLfTuWwvC",
		"12D3KooWRvMvpQNZzgYfqbJmqLHLTYpchELrzEr97MBJ68edxhAi",
		"12D3KooWMSqupP2hEQwXsTfWRTDhtkyLScP2u3G2GXT1PrDyn54f",
		"12D3KooWCz2rvAY2sNDEKwnnwXy8H54QhJYPMDuU5TAEvKupbBGP",
		"12D3KooWC1qA4AiCBxAXP2XkbqAchwStQSH8UCG8HCDtcU18i91T",
		"12D3KooWMRikQkLRAL1zrmHqLqnaBYng1e3mbFjSaRWcgiydC75r",
		"12D3KooWL6yxY65z8HJqieXhUPFSWUnE4KWe211W7rows5JBnj4Q",
		"12D3KooWG3HZnAcmPnGVNvKPXvQDKFc4KYp5jP2bpXwGeTuLQuhw",
		"12D3KooWFVx58aB5aMPjr2ULx16e9N2hVqbQQurdug3WsiwdfaBo",
		"12D3KooWD8ATz51EKXqx3PuKmg3bhcrTJTXXSdrspNKG5c6bLMre",
		"12D3KooWQ3U4j3Q4UXXBQCevuXT7U2qzUyrPN28gura5jF6VbnTK",
		"12D3KooWHpaqUPEhADuVLTNHd27n2SwgtdUM4bqrW1T4HU965Kf2",
		"12D3KooWAB9iUrBgU7v8bCUBYJoSXyJLwtH5u9ffLmJC7Lo8zhXE",
		"12D3KooWFeuXSvcZD2ULZLtGaYckHJFNZuy9iRxXJzfrxKsyMrr3",
		"12D3KooWArQXa4BCTurADT1ihG6VFzUwpuVc3gPgGn546bkae3bS",
		"12D3KooWC9U1ZQDvpN1NGH25FMCnD5SJKcaaH3wgwuguGge3RFUp",
		"12D3KooWFxq9Hx2jjdb9hKeAFmssZqJAoHTnZHKNNA87Ks4r4Pn8",
		"12D3KooWFTMPZ8HohHfcQFpZPJdLtevK6KGi11fGf9L7StQmdu9r",
		"12D3KooWRjv4FjuJbWvKSzEZ38tva9M6FfMN1QSmWRuWoRqnid7N",
		"12D3KooWMSJrLkPAuek6UTWJhsSMHR4voiK1XKmVKtTLbnR9bgu2",
		"12D3KooWCn2Zw4xLtJQPiTeipZn8DnZEgG9RfprnF2wJrF9kPBXf",
		"12D3KooWQdqAvGChzf6GEVhwm8KAV8z8h6s2abMTntUY2ERUarWX",
		"12D3KooWPUaQ81XcQKoTExSvV663BJY2AqvhX38cHoKu4iQggSQd",
		"12D3KooWSN9PxumTRNkhXocKfafFgjSKGCWcuJ1g46kfgiFRe8Mh",
		"12D3KooWLLBQvjiKA3yii2UqQ7KMovA1bEGJEGFfYfbWPmdWKyBN",
		"12D3KooWPnX25HxxK8JeagGsbyDFgSdRrMoNsD7AHQHWq3W17N2A",
		"12D3KooWStnx8p2VimHqR2i6Kv7TaB4g3msqgFz5b6nKHqLjm3c3",
		"12D3KooWCNj57jc9R6mAcFBUnZgUW3iuWYdCbYnVBhqvewCViJQP",
		"12D3KooWN5KW2qdVbioDZ9jC2cYW2YW29sMLWx7LxoZUCq7BhcgN",
		"12D3KooWAdPqG6FAbs7AhMFrUDNbG31BeQrBcVTAgx5L7o1xC4XK",
		"12D3KooWPfzYUBR3qMLWxMqEkjqAQKhTd62DoWzkCau9WMqLvwx3",
		"12D3KooWC4QmL4cdbygRtnA3vtxQVHYFrEMbQtGWCG9SMwqLgvFP",
		"12D3KooWQxPSZEZWAWDJhxz3mhYFFiD5BsKrguQbtkbgCkZ5o9fT",
		"12D3KooWNKuCCBBwBMK2tuj8nxHqX6er5ddtdk1qcJYrAPxj1VGZ",
		"12D3KooWK2fygZWb9mLM9L6K5jwzz35ESftK8JpsSzsSy6zVsMFg",
		"12D3KooWGPVCkPhaZrnsMEDixg8dgR5pWRDkhDYXCQCsM5XJxgtg",
		"12D3KooWNqXoUpRMWCG7Sdz6NVwxm7c3GYBz6FqHXPA3wWXyyB7V",
		"12D3KooWM1RySydn9TpAiX3Za7YACk9DFXhoyv2Zv6idBMC8AVfR",
		"12D3KooWAhF52zyEWVcg8L8ebxqwFVRmQDwTvezzsjvsoQ2oiM6g",
		"12D3KooWHxd9hV8ZYRQag6Uh9vMR85DmpqMNHHnjMfuZysqMoTK2",
		"12D3KooWQvYU7hpn1U7SnsAKzA8SM8mxu7aFakB8REP8Yuy8R3HR",
		"12D3KooWR4fBkvyMjHjo4Kh3qyW6ajDDgYzpzimJTfnVt1TTW8Rg",
		"12D3KooWPELBLSRHRSuwJDdzQFaL9zivMRc366xTbEZye5FSekT3",
		"12D3KooWLEmToARMVrkxqUy33jTjMdGryBwyW9ymqVrTwU5qkTAt",

		"12D3KooWPceDgqf25Y9u5bg4tgc5xBjXB6YpL5hSFvauaszweDkR",
		"12D3KooWNreBfRVnrw8rqS7wuRz1xTZpUBi89yyytiAHuETt33Cs",
		"12D3KooWPiSQZwFkpRkhac1Ze4bTmTL8g8pcWuxQiBjbi4MT5GFy",
		"12D3KooWHK7UsAqHh3kLtHY2F94YXuWRaqqZNZMW65LkoXAqnof7",

		"12D3KooWF6AgRuYkGj8BQhJ2oBLDcRWuAYSQ5uCfCzqCdSr7byeG",
		"12D3KooWBhUmWyqMttugnkk79G7xh3wrSKdPuugXvKATzZxpUWSw",
		"12D3KooWBCQac27zD3kzwmDZceAjFAEwzR3eSF6ttcw8Q8wCPXqC",
		"12D3KooWEwijtUeEwzrPtPFTCHYDZ4hyGFT3D7vpvf3NwTEeFZ4C",
		"12D3KooWGsQvkE6PXQc7AofQJN6ksvyRq3aj1jFottQZm5HBidbL",
		"12D3KooWSwMQRN6EbKgVusyCsMX4EnWhLQF9Ea8z5zAyLRa7wZ84",
		"12D3KooWG36qUbCj9Bt7GxLnfXniZVZgm1sLbVRuk3WLxM5r47is",
		"12D3KooWHwHyR68R3pGGAUJSp2RzftzrbMv1hRscpRakeSPkDopW",
		"12D3KooW9tn1sLSZgte7NqqLit4C4hm5ETDnyeJuwvTdWAMv5V6U",
		"12D3KooWNEAbKvQhtpXjVMk4xQkfzGtySUMZDCAmSF5azd8Q2PSL",
		"12D3KooWAemLqhYSZAb3H7FG41EaFge1YBqrMBqnwwdbVvH6bm47",
		"12D3KooWLAcX7311Ji2KTzpbtcMA1tdFZFGkoTeaNmRK3F7KjwKB",
		"12D3KooWCX69DUBvibifTfKCMsWWz3ZEi25DGifpfsxKKpdo7pbw",
		"12D3KooWDkQf1de7eGvFrwW4hUEpgkEaHmN9Tn9mrchhdsJxByF4",
		"12D3KooWNuz9Wx5pT7yWnNYhp2giqRgPgGBu8C7CtBSz8FmrmUsr",
		"12D3KooWG5WRDHfC9D4Dh6eY6mrKQUwChztXPFRwDPjC9UXGh3xp",
		"12D3KooWCe8PU54YbcVhRiVeHnYqRYuWWJ8ARJGTaKsxCmGZGpK6",

		"12D3KooWJwr88FgDBSys429NdfuF8EokCfpB8NfguWttAsecinG5",
		"12D3KooWJXfVsWgYrgKq6eWNfbqzubePacZT36j1rgZ4iGGgirVN",
		"12D3KooWFa5qXuaW5ayE1XQ4xgVmuTjf9JGqM7dbwmfcb95TbEqk",
		"12D3KooWJUMRndQBLy2K9YZGJDTb8SpXtNN9mRQDJFCEPmrk9Hpz",
		"12D3KooWL1DY2prYFLotpU6NVwkJ731G547h1REk3fXnEur2yXTY",
		"12D3KooWCuVe28APYbvdZGgE7QJdMX2Ywg2nBWPRQeeoXwzCsbnZ",
		"12D3KooWPuCpApFc5GKVvyxKv5ZY55UhK7o5yDvMoFCzVR1auMvg",
		"12D3KooWDQ8KWGCWKvcGfeDtmNnDPvTUhG25qCAq86Qc1QmAg5Gc",
		"12D3KooWMaATWajM7YzqCbA4WDXZSvhcenJh64WWyEgdLFf3Q43V",
		"12D3KooWAcQ6rk6xdweqKFUZW6P3SMSSBpwhricDq27bRVUkzzgi",
		"12D3KooWHmwkYxiWRnsxyD79iTDBxqvQsEysoM54ATp1M59zUthn",
		"12D3KooWG9obQKRYzNFhw3zQJU4NYGAMnkuPHeagzz7qzqQHL79d",
		"12D3KooWGgtxE2B2pmhduPbUy1UXucN6Sqk65PxUvWK41bDcQZLb",
		"12D3KooWQUEFrxAkogqnBzmNrds2sS77JKncF3MpY2uPow5xoamR",
		"12D3KooWBbZmUuF39BSRmVcQ3v4srGHnDLyn3xrj4SpQb12FJ7B7",
		"12D3KooWDYfSz27pg3Uimz6xgNEhi7R3mScGdE4Z2dTLAiocgzqD",
		"12D3KooWR7oc2UQXP1AewbaCNEqCn2mzDzcRgPhVaiqkQetXHceS",
		"12D3KooWAm8eEzYCAgxrZ2Tze2huRyjGfCigd31RsFRJzSMaiuuH",
		"12D3KooWRtBrjDCFqc23uLyoh77gNbG4KbPXhS1vZpsSA8nFssez",
		"12D3KooWJrfHaASXPWgMFBn6MJ8TneGXr4mSz1t4DjmxBETd6Mty",
		"12D3KooWLBKpZFFBp1RaSmGsFdfzFjLfsrpF6y5vU18CCuQw5PuZ",
		"12D3KooWKgPBbpRNiHgVF5tcKT32sfmjvUhDKVRXuVVCAyNqSqiy",
		"12D3KooWRDosqBEP6KLi3ht82y3DJfNAmCi8xigWhqMnA6BAhFoF",
		"12D3KooWMUddkUnVVDJHREVTFnvxk52UeVH7uMYBUjCkvTCw9eR5",
		"12D3KooWAdXB5LHZmWd3wNjJgLPBkVNF7WcgFvLKpzsK9f8sPB7N",
		"12D3KooWNkgd7HmqKevUocJ9SA7NJWJttdtLocet8NTYGYfmmMzv",

		"12D3KooWRHRLPJYLdFEhnRPorzn39bXiR2Q82aDT6ZA5UXyq7U7A",
		"12D3KooWBASepmTQtATM196L1wWH6kVd4TBgpUN57koyfjT2vheX",
		"12D3KooWMH5GBmtdLYimStCdAkLhESQjFD2ZbE2DWneFCYXUZk86",
		"12D3KooWK96Qz4YyB8y4mrkiPp5piPeEquYYGTWz9RuK519qScmZ",
		"12D3KooWEoZ23J3Q64qEHZqqPMMLUhFVHMugt7wNJPnW6a2oPJGZ",
		"12D3KooWK8KvnYY6y4PSKXQYsxqzXrouZ2ygMdg3B138Jk2kJJKe",
		"12D3KooWRQEXtW6aa2LFpxAw2Pqdxfm6M1bEDyGEPHioqHEoYcRt",
		"12D3KooWPq9gPaFT12pTCoTsRFkRNZN5acjXaJu9KgUFpK7wEESR",
		"12D3KooWKCB6xKhMvVL1v3wu1uXAmMdPPBqM1RQ8kLaWPzwNZZgK",
		"12D3KooWRAdMMQx5RHGbYsUaftNJQjoRLyTEFgP8PQs1eaCCz3xo",
		"12D3KooWCivHPqkQUV4UFya8g2kpaaUVHuU4wdXTMX8VV4U1jx69",
		"12D3KooWMG5gEXZ3HyW9ooCU36SQkduB6WbKdaijMuqNy4Gju3zg",
		"12D3KooWBWq6DSpyELUK3Hco8esbEGTwM6Tqzpb6RsjgqEawi3ji",
		"12D3KooWMc8vS9f2LkXCcLmkyK4ckgcJUVvn2drbQT4o144JHiSE",
		"12D3KooWF4ZA89d1pxbv7g5GF36himR1bLk5pd5hT821KHzu3cRH",

		"12D3KooWPe7482tFpQhDg9VzEpdybqXw7hPZKxaRNCJufMr2yLM6",
		"12D3KooWAGkETo4dbvYkeYLhehRmjo5a5bTVKUeC6CNxotLtawqv",
		"12D3KooWKaNmNZa5JajzERWT15iYyY9PnNpHgDXo4WZzq6JHXpAe",
		"12D3KooWJfSqizPVsxfs6fkeAvPh4duFa4PhoC52TL22WTKaYkpT",
		"12D3KooWGR3yXWiigfNN4RfsN2dKZwoc6t5JWuAAyJ4iESqWaJBa",
		"12D3KooWDos1p7xqzmhPLqpD83vxhjiZzPHPP8uEA2bvRoyb7S7e",
		"12D3KooWQPbUbtNh4UsdARrv3YvToinF6qxQ4kDEk8jREuDjdP4d",
		"12D3KooWJVgyznGfpNTEkEbwNWEYPHmtGK1fysunpP8GAkNgvZNQ",
		"12D3KooWMmrW4ZqQx8m3kRat3GAmQkNMNoYufeq5cCE4YpoMW3jn",
		"12D3KooWSAAJqmm1AN33PjjeiNqpP2LqJjRwLyNokYY4FwBzdbcD",
		"12D3KooWDrsJcAFejeEVxhmhbkb7npqtFcRdLJYq3gN6NmerKdub",
		"12D3KooWGSxf9pPN7o8KsnyVm64ab3K8aFcaa6juMr2vCahrAm3P",
		"12D3KooWLkFoNDHxP4QSWse8jA4igfTJKaMPdGkaSUihVVJ1kdsN",
		"12D3KooWMFmVtDdH5w97QybYGgX83GT9rnwhmAzEg9ZSgs82JaVB",
		"12D3KooWMDMCKGiGuuBaDBFxyQBT7UQywdJipG8ZpVLDgZc",
		"12D3KooWLFvsjmvvoscmuDaQhacgwJsSeYLT5wcpevbGQGi5KyHQ",
		"12D3KooWEybHPksEy72bDoDaejN9jY1A14sywmSJbV9MtZhdTgox",
		"12D3KooWDAQ3pHz1tdsbR7cAMZW87QLZfptHNCLK3VFQ5TV7mrES",
		"12D3KooWNGgWRJ1EccawkVWuph8r1F9z14GcdA1mvqqKnZeKxPma",
		"12D3KooWJRM4vm2McNS5CXe9rN4eBvMsAzeJbjYjz5ycvXniWuSJ",
		"12D3KooWBhoqbwcLc7AFjGryQjixvcNsx93XNfjKo8NmguXrJw5W",
		"12D3KooWBeX4HjBZJE5i9h3u5oFpMLcFQruMK1NreNw76myBTbWE",
		"12D3KooWBAWu5zmwYVLjpukHQGqfKqFDqUCXsTyW7zAb6dBCejDp",
		"12D3KooWLFW423yQ81KkexBMfjDCGVmz12gANCENGjXTPy62VuqZ",
		"12D3KooWCkaXxkFDzL6jJGqV4WnoMw5Zor47PAxBC9BjHNkadkoE",
		"12D3KooWCaeXjZXoqEAwfmRF2P9L4stmon6beCwqKaNaHrr2MS4L",
		"12D3KooWKFUVPZyFdPmAzk1H28igSHarfeaUVCBLfcQCZMQdX9HM",
		"12D3KooWNxmg2BWv7JGv1Q4WFY3QV37cbKayFvPxirwVMkWVBnZs",
		"12D3KooWKR8gnoR2oTYpesdjnvTt6PB9QqHCvYRuivc7EimrR2Nw",
		"12D3KooWRd1NaRfkyTsCG4Wvae4ENWZkNvPMQAD963CpyKzqEjGA",
		"12D3KooWGuEBXgsqcLe6Ra9kiRDXN1ApnVAjAoht1LYfVdGHjAFk",
		"12D3KooWPzhfdgEyJc8wiBeQzv3ttiTK8tG3aUpUprh8gNuosf1b",
		"12D3KooWHcYX4LxzVhujDjWbJKPwBpq7SudFSnDv5y7ZKuiXtntn",
		"12D3KooWSg7buUHR1m4EaTgicpdXMAgqj2SPszKrQ1FSvxkZZ8oJ",
		"12D3KooWQyHaRnPVUmFxNujKeLEiwPr1zjMRYCd1veB1Z4EpmcAa",
		"12D3KooWMjXdp2S2cH41rfYn5sGfrLB1ZEKzsdGdM3wJCTCGJBUq",
		"12D3KooWPcCHPdRAX7F8PBUntcAJMggyAVDnEe5PirjSMwkmYXbg",
		"12D3KooWKFQYLGMCyDqvsNaKFAAJhUZE48t4nBBdwPsB1o2BDSTD",
		"12D3KooWKsQwdkqk4b7t6svd8FeRpWcQjVhx6HW5dnJVSjFfiGGq",
		"12D3KooWHJfNBgXdsJ3iHtvTARRsA513muAMcjNvYq4BTAufiiya",
		"12D3KooWARqtBeNH6PKkCPMpW7dV8ANZjeKJz6pFPQA3AriaKcSL",
		"12D3KooWJW7PxsEZvVgEYtgdFhYA3w6NdgdhFdFBgqBn8AEXkyGQ",
		"12D3KooWCsnB386zkXytFAAitC7uvJpZiAJPzfSCQet6da2NUNyQ",
		"12D3KooWSgN2FvKWzdBjDiWHQNZgpXLHF3W4YfCuUWUZVxrnFg46",
		"12D3KooWJLxfJrh55rpG9PmeDhpFa5D2KsWTALKHQzvyHfEd2GKj",
		"12D3KooWQDJCdt2WCCYQEhg6U6qYjG7XCS6VQvyEBnDK8UwiAwcU",

		"12D3KooWSSeedH1jJrvkVgnqJdWp9Xk9St6swwaCoM8k5oteF8iK",
		"12D3KooWBoPju8uA4Ftuet3NYsmPGVKo6wnNn9zMEBYdSSFT1jpF",
		"12D3KooWL1PSKnV4LZxA8DqAeHWP79rCAHQeiJyfQ2v5nBx1Afsm",
		"12D3KooWNe6ECEeHvUjHiSAje2zakgXPdoWUpmzN1PCSSkBkK6Yb",
		"12D3KooWJattYiQDbV4o26CwvBniLXLY9ppBUHmSktpv5YoC35bC",
		"12D3KooWDUkvz9seax4irBeqoUxLUfHVw5mSTzfTszxPgwPgfvUW",
		"12D3KooWT1smfvz351auLGn4zAQzrMYpFmdrKEp871TXbkEDy42R",
		"12D3KooWS4MLCVuSADWH6mDcuQEWrQaDskFL8YFWLkeQrRr1LC7k",
		"12D3KooWEAshfaoFHRWE5JrpU7YUheFxHZGUWf4KomyvfwuitdJ5",
		"12D3KooWEDfkcurjLATq9fr3e8WEYZ56ogF5zJd7yyAS6CTtaWGi",
		"12D3KooWNkXPErXsoZeawZoqd7LWfK5D5PhfeiRzcaPkF3bUm6MF",
		"12D3KooWDAQg7xuU2QdotvFxCsPWMzYu5PpgJ4fyMBWg38cw7BrZ",
		"12D3KooWKw5Qov5dJkoSBBzsrKqfhC6Um5CwUYnPwcr1QvEogX9A",
		"12D3KooWBHGXPLQwJn9SBmxy9AeQAzZNcJQsocAyQ5toH6P1Fznm",

		"12D3KooWQ3AWsrEQuBtteeqa52KepwwsvsKtypZR9RosRbn5hmWF",
		"12D3KooWHyQDrpkKPxms9QmMcZK57npAhoeCqjnLZJiF1p6wFxbh",
		"12D3KooW9vxciHUp3dLdCmnBEz4FAsEv4Tin1q42PJZZATQQzBmq",
		"12D3KooWARVnGZsAUCUZ9UhU94tsvW3caK2WpCnwpqUqkKeVNJG8",
		"12D3KooWEsdeSg7PVikJQmSSdUBHp1f9uKdEguvRZrHPSGcspa9y",
		"12D3KooWREncA7hMmd1WpYUriLiPaaGonbSENuNmBtmQqhm7onrB",
		"12D3KooWSHszu3dMcf6wbNPiYBEht7yz2t2jijNMMqKBga7F2aSx",
		"12D3KooWAcwjz3ftsLtJ19c7DArnWdh3a4d6QhxiCUvNxdEbZtVS",
		"12D3KooWCtpoEjkM8meUsi1ND22wK6krQ8xXP12WSWRL3qQ4KdER",
		"12D3KooWAvQLwgEbwmNUL2J9s1ZTMKvYw1ynYxEWYSnQoMPFDcNN",
		"12D3KooWB2aMBz99u3qTjBvsSfHgkw9CKKszaCxpYLA9NN64r5dh",
		"12D3KooWCEWuAnQ8S6F2n9z6N4NC7E2JnzAGohMEmuTwsDbFVicr",
		"12D3KooWBiqSMeiVsGh2LMDFRJup2WX7yggpQv9pSDKEaAd7DD5r",
		"12D3KooWD32w1ssnHtHCmQEtW9Aa2pSnGPNP14KRkuVi1zRpvBYi",
		"12D3KooWFZDG3Xv6Joq4wvTwbJ9z2UdErpEEkVa6rmFBcu44aza4",
		"12D3KooWAffS4ew7SJm2GG99wwQhqQ5K9CJiKA3vcQo3UNgyUTas",
		"12D3KooWMbqWG2E92EzinibMMkFp3AttAfREsukpGSS4jm8aGKp2",
		"12D3KooWQ6guNLftf594vLLP6nNYKS6v47ieg5U2pCQKUTogrTZ7",
		"12D3KooWP7uD5eMuuAEMymQ2DGKNva3FovdiaACWnoRGL5hHQ1AQ",
		"12D3KooWSVBLh9dZX1ouP5y3AYidUcZxnwagT3yNNpoc2zUxvJQz",
		"12D3KooWMt4Eo2kBdcdCQ3ZXC28tHbgFditNPqiu5mQ6ewPc6UA4",
		"12D3KooWRdS5UuZJFL6CMXwihT2qnVrJtAjMRZnpm4fmfYDyZvna",
		"12D3KooWLJgZLkUWbKX6YELz85a1uLyzujqzHzffNH8k11JwAQVP",
		"12D3KooWDzmg1vXwrv8VqefHYrWgYrLBQyX2fQGF98jxZMshYETk",
		"12D3KooWKry9fCKdvn1Hcd4GKgWR2TogTHmoLYsK6Ef8DJbmA3hu",
		"12D3KooWJd24Sudj6FipeTsb7tQwQTLCYfQEt4m9nJwQGgtSvqgz",
		"12D3KooWFjKx9HJ5tfRpf3vFXzQL9S9TXoSMmgkCCsCbLZxTE7jy",
		"12D3KooWHsSGRAMG1QW1jBVDkNzujRwLbkG1eyJZRJbeZspYdWbs",
		"12D3KooWRAGbNhY2LwguFtQKypCshwD8D3AydyeQcnojVfL9mxYN",
		"12D3KooWEPUq81xWRcvFkWX9aY7ET99HtxBe6ZmvW9BQ8zAAgMqB",
		"12D3KooWSXqCR22L9EDaNPr9i8wCYuDB5147vyBFGKL4jDYm4RYR",

		"12D3KooWL9XysuaZfX9UVvqjcxmj4qR8ddQNHT4xMDf45RDuzWFn",
		"12D3KooWB5wXwtjN7d7ABXJEMBhxaVtUjdDS9JgvsLStf8fkikkn",
		"12D3KooWSeR7MWBe95AtJKc8UYPttNbCFEzXCNTuVhSag5kX8dVk",
		"12D3KooWBZMzeR5HRiRiE8bU6V8jdgtEXFEKiGVdxaNqKvQAVzmS",
		"12D3KooWQLHURva3gDnruNV8t5oU8f2ySoJp1GBuucYxbbKobkMJ",

		"12D3KooWBc9nKfmKJZKqkto9UHvQce1ZZZy3MoNdZzDQpgi2SFkD",
		"12D3KooWP5nSvoTyGw26Z5g9ubEcnigrr15nfqwfhYVG4WZM55WH",
		"12D3KooWLHCKJuas8q33j951sXEjh89xsAewm2VSpdsg4Ldx5tzA",
		"12D3KooWNb8oPZSKhoB77cnmGD5iaZdAgVKx5wzBGySyReQXP5jr",
		"12D3KooWEHrZx9RKgvuKDRPxTr5AQwrKYU77ecisL7LwKQfASVNz",
		"12D3KooWRWnjEvqJwMC2GckGu15RLTGD9rErjUri847r2Zdw87qE",
		"12D3KooWSFL4EotC2ZMzy1Z2xNvYSnahFWChC214Jz33bP6csAbW",
		"12D3KooWLyvz6WTq7XHsAsSVtbsgaQ1eHYcp3DkUCuJ6wr1qLhzc",
		"12D3KooWEL8YYtehfx5tXQwctP1NFtegN6VZ8Xb8SFNfXBuwGmfZ",

		"12D3KooWMD5QU7nh9UyMEKN4zDkk2mXLDbguJGsEiwmMsq6dcrRf",
		"12D3KooWLMXM7CZiMkP5tkzPEr6CRbksaL6jiaYZScoJEw2NUEx4",
		"12D3KooWRqLHVAXaFhaaxtUJ6EsAMfGwJCE3KjD5f8Px5JGAiPwv",
		"12D3KooWHTeDL2xhxmD95e8RjjqU6XP6GEjw3RJQZfcvoVWLfAx5",
		"12D3KooWJ5FGSST72mRm6pMUm6bzLzDyrtnLMbZYz31bgqFK2dRY",
		"12D3KooWS12EVKDtdT6wAtNXeJWiDNQp7kf4gtUXGyp4CEXwomSG",
		"12D3KooWEH4ANjWzfRCctc7ZRqNNvEJo5YCaRUZTxVEvQDQ6yaga",
		"12D3KooWH2gRPduWubTCRVFN9RMt3Ew8gGQEf4QyNuTngc8QbV9P",
		"12D3KooWAg3a7fWftpgvkwGTqtSGR6aVEu6vGZGaZsjNEJdt5fTs",
		"12D3KooWMbVrXWuFNLW8dBfK22qofaRULLW4sz75P18gehthjQ5q",
		"12D3KooWBwtVSApjW2nLVDpLyx3m6YjeouD12ofsu7N3iDPwwZnJ",
		"12D3KooWFnB1bjx3Y29FwZKorVKZrMQjAgZDW7XfTUFiyDsKAz6D",

		"12D3KooWAXQTebgY5xuunYvicAdr994gNZGuzsjQob8WqB1F4jG7",
		"12D3KooW9uucsTjDG1k4duz7Cw9q6bFdrk8GuPj8MrhpPRTsQUBf",
		"12D3KooWDvy4Rbfv3qX3FLSdybAuU3CKj9v2af5dmAhosVc8u38e",
		"12D3KooWNgBRAurjZebKDCmekRcNe9KmmqmUjtRryyh9Qky",
		"12D3KooWFQE738dR8XBKsZ37UDptfsAySHnXGxchMS6oicWV8KYt",
		"12D3KooWFVuDMK4FiiNsQmFoW9UQ2uXYt7jGCDuBogM9Souioz9w",
		"12D3KooWN1zGhPfJRxhUsRj2ftvgv5mJjty13KsBkC1WKhSMcAj8",
		"12D3KooWDWJUWz3zRfB2bfXJxK7WPSM9QsDDPeME1BYJqq2h9oTh",
		"12D3KooWMgGp96wQuH8BTZ3AkDnun3k4U2VwKZAWKeynZF3hVTrD",
		"12D3KooWCyGc4ZMSvMvpeXN6j4fvL7R6mur5dbyy7BVtokk8Lw8R",
		"12D3KooWBmwjg73U9SFyw758rKddbE1KufLTzJqWkqQURYM2UPwk",
		"12D3KooWN2ZKwuYQTzqgd5b93LL3HkrXVhCaEegi24RWP2T3bC7M",
		"12D3KooWKtPgsmkEcscZEBBVHZSNQBj8W9axgtpAEn2FiQ132b4P",
		"12D3KooWN43ivYtDgsYsCxL7LZSNFEqDTsQMuknVVTWs2nRwojj6",
		"12D3KooWMVwaFKTpSZYkx4pPNAoHsDCu9HC4m67LB14C1Z2QfsEb",
		"12D3KooWK4GsbLLDPV8KPeAP1kma8ebfF2ZPomp8JMYcbXBL6ZXt",
		"12D3KooWG2VMWvnvXRdf6LVqifLWjqFxpzjvaS11Z86FhrVA4Mhi",
		"12D3KooWEKA6zz8yrtrsbZGY8w4XvKxF6cpBi7Zwj2yXiyVqbAPn",
		"12D3KooWBw5qdzr3S5ap36dEiyCwrF4TuuAL9gEaajYGfVZXPN56",
		"12D3KooWDYw9bW8Sd4n7AgVZujwN97fkgoSEEyQiseQSLYWVwjru",
		"12D3KooWLFj9bbkrjTVPgCiW4uDwUztnYxsjmq6iLsMUgS1ZVspu",
		"12D3KooWR1Hv3ojxShhLzCPqPDDx119kuQj467rak3xgbjC6Pqpw",
		"12D3KooWHWY8JZjsfC78KyDbLdohzB9K3AZ8dht4UVHZhwiJCXaG",
		"12D3KooWEYERJU9cvgmEvSjP6Ju7U9NWibRnkEbCSd2wikTkB5bw",
		"12D3KooWJkrQ6tRXik4gyN6A6BsKd6UsMhMDH2KLXnj6sGXHJJn2",
		"12D3KooWDnBoyjDVpmkVjxHDaGZA7koom4GujuSMxCBuPzJc2vts",
		"12D3KooWKwtb6WwyUcWExncz5cARunnwVCuywnXjdYMyazRfzv3W",
		"12D3KooWKpjKyrgxEyjDCyTYcAn5cFFRsBspMRM16Vga6aUs1VTe",
		"12D3KooWCDVyqJCrsdeT1wgCiKF3JnMVU5JBtR9UxZYTTVg5qFqV",
		"12D3KooWK8zQtvUK1wgqLt6DQbU4W8L5sZmx8d5tRgyANhM1YfLw",
		"12D3KooWNm2AimAgPDYnkY6UFiGAXwcdcW3ahWGUXCco57U1VzPu",
		"12D3KooWBVbpE4grENhaCanbxmsR5YPqSPqk8mdC55LMBGEHFqoq",
		"12D3KooWSmaFjAqHEtLJvijcy617wx8Xa8DiFd6kHhqKmKrrYGaC",
		"12D3KooWSdk1UzgwHZoXRiQU5fyteBrw8jSrcdYu5cCYjzGmc2Rr",
		"12D3KooWF4676bcmV5DKi9yHC743RRe2XF6Ddoag6LxvYGnViguV",
		"12D3KooWBLkxfEc3UnCVL2SqG278u9zrVEjaJf6PA7XGxcBW2RL1",
		"12D3KooWG73S5cabg2jf2au2nHsrGHWJcaiDjg7QQuw8ipkJPej8",
		"12D3KooWFKcU9i4XRFfmB8xystGRRy9B4JAczmVFoWvnwaqSkkkL",
		"12D3KooWMubeomW2o4ctNBovLfQuSf6wJkCdW7k1ir84hDxcPPqU",
		"12D3KooWS6Yw4dNDesGDmv7EW2SwfXPghypde99XZsLnCK8xsSD5",
		"12D3KooWMom7boTsA1BBc37HjEbewjveRgZSZqunNLps1EkVux3Y",
		"12D3KooWGojzFFQJ5eyewAo99DoDmUB2Z2ndjJimMLkX9tohqzck",
		"12D3KooWGr65Y7Zv9UyjSE5rfNUpJGy9Ri6S8TytZHxQbEHREaHW",
		"12D3KooWCTL3sexWppH56ruSyadAEW2uRpKx5ao2c7XeTJJ3cCG4",
		"12D3KooWE88pgrKev6Nec65BKpyGSq6bafaxwPm7LTVseSqAzJVL",
		"12D3KooW9pUC8rNDe4fuZkRR3NdSUG64wR5ipeFmewgnzBsyA1ZD",
		"12D3KooWMSvWE27LfGrA2P47ZE7iFQpi7fYfGmYUY55ptfTzQHPw",
		"12D3KooWNxxr7QPdLsqbC4teHRp7n5YXwJZA56KSfwpJYK91YxP3",
		"12D3KooWEW73vKGNQsLVxfM1BuYL7BZBzRNU6XyEN2kZhj77JqWS",
		"12D3KooWDhE9iW1LQajdzyh6v9pA4Z6FWsA27jyGdr4e1M2TkNba",
		"12D3KooWSJ8vhJZP5sRYcnUsp878cYvRPq4XPSAfQPLZLVhKFnHi",
		"12D3KooWMSNuRVZEswN7BqLQGVvoPJHm1R9HERneBNw2a6825Y8G",
		"12D3KooWFCVD1TzgroBgqVf6ANHQo7Y2HV7pQ1T2jvn879qFAVtQ",

		"12D3KooWCL7wHHGjo9uGu3hvsQgxJmyhMdktQESwW3q2MZgPLAvZ",
		"12D3KooWE7dKdPrTzJzvbTpGZMAEcprXHfTPpADSxfvUbjo1qR8d",
		"12D3KooWGbKAMueVfwiY2p43Qb3gquaXFKkezfSFoiuNmmnVixXq",
		"12D3KooWJt4os9Fe3BqSaQ2sr8hiH7tyn7A6VpdVWpEFDQpCUd5g",
		"12D3KooWConaEQUqC3PnbcHtgRv6xXqZ64qG44DDRjV8JjP6tZKj",
		"12D3KooWRvjVztKk7xtHFzZwV5WjFgUsiMpu3fNX6DTPStcagzqa",
		"12D3KooWKqaCAoKrV8xiXHr9dYKiJRZk3eQ8z4ssj81Y38fFxQRj",
		"12D3KooWGUDdbeYTd84CcEqgpzCLj3au3M3jPoGWBdfmpNb6Kh2z",
		"12D3KooWKtMkheACqZD5fGYoj3x5D2PXVdp5LgCdamkdwk4verFG",
		"12D3KooWJqZSuMdjy8UFs3TmhXf5rxPXxnuNBBdQ98ymz1dMhbcS",
		"12D3KooWFP9Z76szw9kEGGEgucydCLXsYTb8VzY8XWGoiXV7EdVW",
		"12D3KooWRZMz96Q7fvvUoXGgNnP8Pz299v97j2vtucrBayzrK2K8",
		"12D3KooWEPsi5fmRLBSEK9TxMyJrtnKJ2QvFBN698yKGwbsDKeUY",
		"12D3KooWQpqEiAKb4xHFSwVShWH5V8dMttYCj4BZXLGMH89HaGEj",
		"12D3KooWBqBaANmygrC1tYV1P4Us532RtQEjarK7NpXj8yVuBtRz",
		"12D3KooWNzda2wJXk3RyvC2sJc3gmdYJ4fTUoxMCz1ZAgqPEhpYL",
		"12D3KooWPueFJFRhV1iizLNDXx4AAnBnmev7CumhrKHJR6UMKbW6",
		"12D3KooWDR8ENxUuMqP3e4GWozHLeFNCpi239ZbKxujgeKexUJGg",
		"12D3KooWRHu35MYjWtPnAwVraCiAifqRs7ubhthnub7RSTCspn59",
		"12D3KooWFd3bjJycHnid6f3o2939U47fMtoxw9ReqJTDDUKLr9bf",
		"12D3KooWLoJQgX1qn3qSEw4EJvTS17wSgK8kdWaq3C8aZVwzdxxm",
		"12D3KooWBeACUzDGtVcy2VkXGh3yevWqXL3LucjLkdyqJXjVGrGo",
		"12D3KooWDmEDGSCcXwTvciQRb35bekrw9GS3KCutZHfta2MKjUev",

		"12D3KooWRQDTAUQgBnXyxh594V6Zicnw8QY2Pi7CSwGCWtthPSoC",
		"12D3KooWCmKJAj5D4soTYR8KwLK1Ncz5p4YvvuK5gytjx1EgcNN9",
		"12D3KooWGXszRdEiDZpPtuPDHuN3pPrZJGEYYjLvrdVEBgRnR471",
		"12D3KooWGqYVUbzkgm9U4r9WDTwxgKCjRThC4FZqrEMC64joKqMH",
		"12D3KooWRUVp8LWFhrwmUnjWcGF2U5np3Uvt78stRu157XwZWLdy",

		"12D3KooWFc4SLVUmvxzePKEEuGg61dTqCdBWMYkM1tvZf4UGANcf",
		"12D3KooWL9j9H37JNA4VKJiPwhTntr7Ys8dry1X7XfKMFqCEZK9L",
		"12D3KooWJzyiU9y57JtV7uAcuTs1j6oHvpnMXCpuqJDQGLqewoxW",

		"12D3KooWN6cJytX54DoWK3u7YXj74Smino7oma2c6ZsR4sbh8QUY",
		"12D3KooWNK65jHRETWmewpy2tbbDRNjWifh42mo3PdX9m9PhoNqs",
		"12D3KooWS4gADg5kNr9kZw8maZ6MBPvtvreQ76QkGiBJeVhY4ZA7",
		"12D3KooWD1pZQkUWByk2tsx1ZdS7Xw9ivyxiWE79CiHNhhvv2HTU",
		"12D3KooWF7ioVT4tY8kxV7Yas8LpicbfMYgY5hnW6xtVA7k1dRKh",
		"12D3KooWJvMsXQDgAL1evB3a6rQpPS46kpE7iuuM1LrUg5H2as4U",
		"12D3KooWBct27GArLQoZZLwnShT7DnaXmNZpEiK8oQPvNuZrptp8",
		"12D3KooWBr9vmBNhc8Uqy8fqooMxBjgjP7AZoovgCv529ddJZz3c",
		"12D3KooWFFNrfN6JBbvy3ub4Nsy1hP9DJk8dVDieaE4s4EBTPdkf",
		"12D3KooWJ6dzyRBADfcdzv9TXrsVBckVtPNS5gRuigcJMuLzmMpC",
		"12D3KooWA7waEhNak9BmMLDTtKzbMKJHxwqtvaBNJLxLj3iNQHJL",
		"12D3KooWCAv2rnzzJxqzMfcoxmY9x9KBE9xx9fZ6sResdbPFsUmr",
		"12D3KooWA99F5gUxP1QuXVjRRF47e9ZQMadYSYTi1XeRKjBm3TkZ",
		"12D3KooWLKXeqVeYgt6ChPNfRHngKJ8KZUuxB593B9sKTdnEX1Ty",
		"12D3KooWMPym5t4MPDVXXjyDuGR4aw2c51Crhtetva8qosnQ53YS",
		"12D3KooWAk8FvwRTWVPkiXZjVJ8DxdtX6a14PxtYdPhQUhQ6nB92",
		"12D3KooWLgu1AjzekzoPzpPdyzLMVQbznXf8b5vSK9R2K6Ra9vux",
		"12D3KooWGhDLqw6bXNMyBmcWLsxwD6cPNnkE5nMLpGUh86iVWYg4",
		"12D3KooWNHCMXFUtNWhFEydkRdjjT2yrDeFqxZNjdWrn5UkWBPn1",
		"12D3KooWRKyRfWVzezirNRLUuxcLsfkZpHUW9PMy7m4Ltubvgkvx",
		"12D3KooWSMirPvkp7YH8DC8WXStMQWSZBQqv1Wm59kNDmPR3zG4W",
		"12D3KooWKZbXMz2zMhfGufRhygrVf2xt3Ubq1tn1ESTg1ydGDg1w",
		"12D3KooWF4BEJxnSUgKpD1qY4QRRBaXHtV6NctGQrwmwnHKPqvKz",
		"12D3KooWC2MyVDxcTyC2zv49jZ8zfg6ivyA5xmJ5iQW8eykzgW9j",

		"12D3KooWGk8ng2NRCZZsHrUoA1jG2ipzgvsf6egPkRfr3dby5tdS",
		"12D3KooWC9g7LU9DS5JqUt5KAFzw7zqSNTvtP15sXdYhPgWaF8eu",
		"12D3KooWQNw1j2Asxu4H4RTnG3jZsuohiy2u3nbcbwrsYQAHHPkP",
		"12D3KooWLDGvvnRaBNkqvhhFccDqyWUeCA6bTsggHaH7v65ykx5i",
		"12D3KooWKJYoeWPc48uAqjatWLdH9ZGfKkGb4yj31UAgD3EaGWL8",
		"12D3KooWRpnywhyBePKMMvKDRxFkYtnhuyWW8rbrsC95phbYxXtr",
		"12D3KooWQmNnuwuhbjcrT3sJUW6DQ4u1bMg7X6ZqcpGbKJGVLAaA",

		"12D3KooWB2jFi9eAPhhpVRHNpHe535LVwxgqDtpkM5NDorGA5Y1A",
		"12D3KooWS8toZ7KXh3eCnJd9bhRoUQqTr7uYAzW84mx9jCc2xMnD",
		"12D3KooWJHw6SjUvsXcvjANowCfto8gLAWdp4LX62eYUmPUSk3JX",

		"12D3KooWM7dMmihG3SDJB1Na3Pfzwchnw17Lxs4MWKBNDCyNxxpz",
		"12D3KooWL99xaHb1BENT5us4FkgbWbujzmaG8XT4Goe5hZrfbCGo",
		"12D3KooWSfF7EFYoyCFm3QWwy6zVN3j3Hj7dcoEwYz85Pd3eW2fN",
		"12D3KooWBj8px71MQtkwb1zCZ7SyCmPCA7CGefTYzTrqD5UbR8tb",
		"12D3KooWSG2vhbDFgQyoM6NRwd6ksU4b38bWmp1Hxb1RazqRtqWr",

		"12D3KooWLLW4etgwHJ93SpxqT1sEiwng4KedXQykohWasizcYZtt",
		"12D3KooWKvXq9Q459132G4bdRfhdnprmFQbtTz7g6PK9MZn4JpPd",
		"12D3KooWRQJZ9dgFcupZ8kCAWL58imqXy3wHSvvPGX5bfBanRZ6j",
		"12D3KooWJQwLTJJzX7N88jkvMQm49Bajpaey87AQE9jNQuY1Js7U",
		"12D3KooWDoADxKY8cESUPvFAiMbPT9Xay4cxpeDGXQZXtbknxmBa",
		"12D3KooWARYrY8eZFZx3BwsUNbXDiddXDNN3a2PgHPcLh2cPwusc",
		"12D3KooWQXm5PH9U1GumsxoBpymzvBPtkKdCiwmZMwvhoj3bBXaa",
		"12D3KooWPE1KbcQNJPidsQKcRJucBhofaGLXvBMvVNTQrB4LnCnW",
		"12D3KooWMTTiDtx6x1pbR43AY145VGWMYdGYVvXQabAZ1JWesWKQ",
		"12D3KooWJ5LJWar5EDd7TgV2nCCqoQbjvVLTAQkrYXaXf9ue3XsX",
		"12D3KooWEXj2bfecMMnWnbfGPQJjPozGjH3JZdaNSMmgwQWDGUWd",
		"12D3KooWMPWqU6HvmAZabxCX8Gn9WWXUYn7QXZi9cyLEXxyTr6hH",

		"12D3KooWPMAUKS199KyngopQfkXN7Hz3FMtLc74Kq6C7GLw51Vo1",
		"12D3KooWShBe35bVeAcGjTpUNNRREQRnx41ngLtefFazj3DoSKoD",
		"12D3KooWRrRbfwwqEebJkvgHm86McpAGuoo5vMdL233D6Z2frGtd",
		"12D3KooWAGjYrp9zzeiMJUQHZZqwUd4LADsFCStoPUBXbQzCWQcN",
		"12D3KooWPy2AqXJ5QsJs111pmowAUFvtd8RsNsyxPTHMTHiDy9Bc",
		"12D3KooWC1M3zN1eJ5Sh7XWqgvK8M5LcmNQC5wkLJSDNsGceJj8o",
		"12D3KooWHh1bS5W9HUTu9DtQ9w4wUL5gFeqbRyMLbi6LGiHNxrMY",
		"12D3KooWM7bh6jM76abrkSD8kHc77c6n1LukBH6hb5vK9BsfrTDs",
		"12D3KooWRD8bfkRDfkFzDuPL7D1EzaS791a5PAiYpXMPXZudNpyi",
		"12D3KooWPKWPpD7AvWwvqTDjGpTN4SXJN9qa3Dju1XFao2vqciLR",
		"12D3KooWHYAQPW48dn5AhMNTQPxwVT46ba3Ce6vLjtp1S1swdynm",

		"12D3KooWMVWx7aeqf7YegSokajRRMK2djCa6AizosXwW469BdY3w",
		"12D3KooWKGmqbmgnfaotEnzmiV3NH4zYdVq3nD6a58cFi7hUvbDp",
		"12D3KooWHZKBVrgaXYkW2Jcw75Dnh7AoC29Qy2vvyDocfnmsWzLj",
		"12D3KooWJXCuhiKLodTm9KqYBHjk5Z1jUDswxm84ikZoc2XhEuQV",
		"12D3KooWGMKGNa25db8WzYebWge2MjqU98AWQhwk7W5yCCpCEQ4y",
		"12D3KooWJJjPLNamBnvoEiY2TwTdRqtWGUdN1eraQBg5dfy1voiP",
		"12D3KooWKeRVpJxAC1YgGYxNsoNoSKJNGevVqADtoZNgzJABVQSQ",
		"12D3KooWEJwZbaLnAbAqXXuPgiyzyFjknm6gAyQrFGWU1HPanuCT",
		"12D3KooWRta7fL8Jw2hNiFDtSc8GPVNWS6ZEbBG3YAxvyHx7pZ6q",
		"12D3KooWGRQWT5hNGb6d5LcSey96ZpZqw3vSw6NPjmfhpjNT3Gez",
		"12D3KooWN84D6Hw3bqWPyGS574GM1eZs9B1bBh6nJa5e7FxvF1od",
		"12D3KooWJ3qUvJb4yW7Ffpj5R7F3LtHar8Q8X5zr1jmq2pcEWLZ7",
		"12D3KooWPBcxuSywnam6oYi4Tc1tA4eYod4BGNR28VdcfXjNKD3W",
		"12D3KooWMWgPrbj4q3S4VQmiR7kA9Nry5gTdFUxGmwiQgcEUeJfp",
		"12D3KooWG4PufVEvUptF3q3pcABoMq2ttXEdqBTBMa7NdSR2ETYx",

		"12D3KooWAUUt2C2i2owYgk7tox45tnJF6urEDBPgiH7ZmxgojbmW",
		"12D3KooWPSZ7PjdvsyMoEAQKY71zCiP9Pg3GJ7G3KGTThThmyBm7",
		"12D3KooWLvV2HrtPuAmBctWoHdyyJ2wE91D9WoEikhnACcUBAddv",

		"12D3KooWC8ceDRexGoP2w2pQNEYERn5sjD52awB5UofB7aS8iFPM",
		"12D3KooWLVrsctDCRsEKTch5r9Pj62tY87VNcLeoNyi2p44FnaVE",
		"12D3KooWMAkesGRqkQWdTQ6dpPPpYukt5LgbDCxWq8L5PC34WBBq",

		"12D3KooWMGYBUa2oMnBQ3YQC9QbQ1kmAGBRTgBWrGNgqYdAPxv5C",
		"12D3KooWE1nvHcVEzAzhkNSFKNhxxFQYYh9uALww5dE6SxatFfvX",
		"12D3KooWLsH1S9jw5jag3HJonj9WkUTpd3jsuCEGknAkpuD8RLe4",
		"12D3KooWPysijQvFX3NwLPgtfmHPiT1qUJs8fzGqjdmpFKAZjqQy",
		"12D3KooWEx3uUe1YYig5v9WK5HH228Xn2pqrmMT6UY3V916E5GS2",
		"12D3KooWGdR4HpDQ4RsXDiBY6sAkjBpFecwchWZDU5G8L35RVCpW",
		"12D3KooWCV61dtrSkykyEJ4Z8i6LTgvYhaWVuEjChs8Crk4phLXJ",
		"12D3KooWStZZP27hWSbSwayYkFA1hgXLeHNjXCf4LKKrJLX4cqjR",
		"12D3KooWFXQcAodkJW6y3BS25FurxQ8pSvT44hL7eFSJJprTcAPE",
		"12D3KooWN9o2HshzsG5P91E3vESsomL8gEdCsrPaHh3wphNHEoLW",
		"12D3KooWG3PUVAKz9ao4yRLVsKjmyhHDUL3NH9KbsFBfVmmmkoaC",
		"12D3KooWMWaee1eFTMrQnpQmh5UCAr1HXbqixQaP7poDb2KzX6fY",
		"12D3KooWCMLfsbESnPKApmsQn7WtPGta5QtHc6yCsGgP7v93dUJY",
		"12D3KooWHtEzFPoptoMTs3Mw3JQHvT29Kee25CnMQKCuTje2eRgs",

		"12D3KooWRhiHDdWCdPzTDtLsyYhnpS4D9pK3EPsiGT6nSQDyfmn9",
		"12D3KooWSgzwyhPuXsdKecbLYdGD9mf3Mmkr3UkKVrpyANE99Vxq",
		"12D3KooWLQrtS9VB28akua2BxXLkQz5GW9sfBEUGLKQsKpSPaSTx",

		"12D3KooWNhcvR2DQTsoVbGNLo6fjDbaWz1HqReM5TRcRXUUBbw8b",
		"12D3KooWSZGqfQhS5erAkfN89sg3dqnzSmPdDT55iFh3vTQK74ii",
		"12D3KooWGxjLnJCja8rqtGK6Dt5tPC5CeqrwXBvFsBeUgTQDjF1T",
		"12D3KooWRixkM4oNKgaNCJrBxEZcRdfpNUybBfLPL5AWup9xLcAg",
		"12D3KooWDk4QApcpR86PtsWa1WHhHm9uuGvL6ZQDzVzrLUEk357R",
		"12D3KooWCwPLgiVKzP96qyey2U5KJdFRQvrijqeorMSj9tTfG4ZR",
		"12D3KooWNh8qp31SVaqcv97SztqgwApNiPYPvTGYX4B25dwFEgMC",
		"12D3KooWQVE45QWDxa9s4sNhBDNL77dUePmTiwQuKETrKwGFXi3h",
		"12D3KooWAnjKu6mmLZHmWJmFa1X1gX8FqTe1jETd9Q5mFRBSCdqT",
		"12D3KooWKbSA4GWsCGhsyw6eRcWcYUWZzpCZNUETEYyKTRjCQGx3",
		"12D3KooWKUHhBi75wPh7zMCR1ggNP5ux2mwcFFYftRy9vr7Bwm6v",
		"12D3KooWGDN4XBmcfbeQumByWPFKTVGeG76RVtrTL5a8VXhXcbf9",
		"12D3KooWLxechMS9dfSxovLyFpGH7Gb5kjNyazfJrHZLuaT9covp",
		"12D3KooWMfJj8P5px6grkmsnFsaNX1M9E4iKh1cJYfQ3koXad8oQ",

		"12D3KooWHPf5p1B5dQ6CaaaKiaYSn9sb5V2gngGjtn13zbpN3UXL",
		"12D3KooWMp1wtugCaMbyUsquVzVfh2epNja2ES1tcPR5orMY6ir3",
		"12D3KooWCVcyURR2eFi78HKpMHVcucEHVEbX2YyayAEhBE7KJDcJ",
		"12D3KooWMyFjhx5dNegPimeV1nyp7k9XRyp3tsjKPZxPqSmyxfx9",
		"12D3KooWAHWx7zLv3Jgp5fTrHtw9Z9miGi9KZhsSRgbQSmrv8oNq",
		"12D3KooWE7bABctK3BfUuJPgR2p1NtXEGBgu1pLE8xPoXt9L3Zs9",
		"12D3KooW9q8TEueuHTQdAWwsfc7b4gqwmkfmVyJTdesWRckzowwu",
		"12D3KooWKdk61n1VyVs6uHW6uXPDdiATjsFpsvUkNkACT4TgDp6J",
		"12D3KooWGZn6ysiBvk579qARq8MWiTDsuHGgJwXFi6n5dTgDqqz7",
		"12D3KooWNRKvCMJrhAuWmadZePP51SpKfrbDfjjq3aCy1VQ9dj4X",
		"12D3KooWCYDMP6p4Mzpjr3axG8estawiN74fs9sGfjxdXaDt9QW5",
		"12D3KooWR3v1BoTfvhJoMSfhxLD1uDzkNktQZv36m3LonoUjovvc",
		"12D3KooWChkGvaStLVRhxai8ycFRNCH3KViWPQGY211BRfgQV4xn",
		"12D3KooWPecFXdUnyTtSabzFDNdN96mNAfKtA3BT2aHHzmJfqZbB",
		"12D3KooWBTkqApC8aG1LCBYxuG55nkushCasaGbp66y6Mk4ada32",
		"12D3KooWF2mDPn53hN8w6AevP6nuuExPDHtdynhAXkoczjUo3Um7",
		"12D3KooWEx3r7seuyJYm71AdaoBD92dEyxQEXG2LAW5eSZaJ5BYj",
		"12D3KooWJh7TJ2HFqnmKrQNzXmTZWAVDzfAzGEgLRSYcBJuRX2xH",
		"12D3KooWPaZrcCtcMGm1EaC1Uch211udeeqyuCUq9bPf5GjCiCkP",
		"12D3KooWEuEn1i72spytVeTvvf8vyvkz8Hq8D6KhBXmZUkHccNjt",
		"12D3KooWAbEienzfXx1ySBXUMWEegN2Mkihu4DBrFpQ47UbEL7NU",
		"12D3KooWP7gWQ9oE1wd8NC7MJE7QPWfySrvrG5viRX2SyL53NRdW",
		"12D3KooWNu6BW62hBSuSUbW3fjgdgNekpUC7ETs28WNb8Jkgy47q",
		"12D3KooWKiCEcYznuWEDNr5EQNLh4mS2VHhtuPqNS1PJJJbah6kW",
		"12D3KooWEmvQUrXEZNso2V9EvrBA43efsJxGsGEwXLpohoHbikyp",
		"12D3KooWC5gfx3LmvRS96jC7QpXRg5nF2pextta854LVzVRtoRnc",
		"12D3KooWGduFD7VxGhV4A9WBMutyH1ApX3wtu53qPkEt5SMVf99p",

		"12D3KooWQGhJ5exBMYqZHmrQaPXjLZHuoQZLXZFWxUALyJYu8RHT",
		"12D3KooWFHQJLVqofP74sCZhk3GJGpK8kvGx26PQKug419ZSSdJV",
		"12D3KooWBYBxgohTV8nPkqFqztcoQrECdCXaPH6TvG1aQKK5Voao",
		"12D3KooWFJ2w1phtV6dEU9X2PSNVFHJmoy6PNRfKVqdBPNmzP8ec",
		"12D3KooWEvvjKpd1Lckiujpt11udm8gtowMV8fmaf6vdFPZ7dHS4",
		"12D3KooWMaCpphd8Hfm9vHMLaqK9udmB6aZzUN4imoeo5sQnD6eb",
		"12D3KooWFEmfeZTgtG8dN9uaZBAwSUoDedFdymu8s6reDJjkJdCa",
		"12D3KooWGQ7VZJMcnVKYEckvYtrusg1f3kniDbJGMp6fUuwF3Fbw",
		"12D3KooWPfGrkWoKEe25sy6T5x26xoBgkpyyHNXYMtYoCRPxsbdV",
		"12D3KooWQQWZHQX1qpCTj9AAoekbHfEeME7icurmr7xJjdtZHZeL",
		"12D3KooWPH5aF9z3AVzzegLepwLp16Nd9FeVBg3YKLKwxMpyQQwP",
		"12D3KooWQx5rSqobtHv7MxkShxkRnr6jmSd14YPfZbsKjVm5DuBS",
		"12D3KooWANXjjvTV6y5zfDinxSaLGSEA5QxoubF35mxwHW58z2ab",
		"12D3KooWKoX4fwK6wMpevMEGeQdM4Z6jf4XSK5osyyP5NNbkWchZ",
		"12D3KooWM1gUBua1ZenCpzfa5KcP9ARF4pfzujaNr1oMyHZGNNSB",
		"12D3KooWKfwiA4N5GSstXvfy8GtX6WrVhJUonjXqasy6DshkKtth",
		"12D3KooWP226iyzuphYY7q5J4WQFfPwM7ue6MNiCccSAbcHLVWVe",
		"12D3KooWMyvikfuQJDYd9tz1G6WwzWjMjTqJ8LWcYJPosPKs8HAJ",
		"12D3KooWHo2pW1V57VMdTrM28ycjScatNvXs4GdY5ChdwiyzYjdV",
		"12D3KooWF45SCeaYr29iUJi4rsQAboTgW9bjcckRVvV8iqWhVsnN",
		"12D3KooWJdD6US1JRT8juiKZWhx7TvkfPFxZZESVVSyGo2HCjD1K",
		"12D3KooWJE1HwBcGj4u16EFfpCUbef6V1o1MCtP4ndFLohxcysG1",
		"12D3KooWLJXp5yt1v5Ura8WqeAoCXrUHMDCrisHCrgtqpiWot8wS",
		"12D3KooWAMnQmEXcVnMc45BXLM5Z7HG4MDV56uHNJHxnF2tyMbRb",
		"12D3KooWAEn71Ud5aTw1qckcgprhG1rWqJNB5uEcmeRfMaMi9LYG",
		"12D3KooWCBHfjV4NXCySpxGih1nyMSX687abnJ9XvYUXdsytvHXc",
		"12D3KooWCf9gR6Cm3KJR3cDwHnf1jiyT3cpjMaJYBdbsznWadehU",
		"12D3KooWS47idXvbzQurhQezdnGpAvNniQFfcYEviXSEZeQq2e4m",
		"12D3KooWCAMGzWrcnbPradgkJEe2yTEWNYi5LPM4YqohDxDdE9Ng",
		"12D3KooW9tE57CJLvnM2pnSADY5Yq8fVXgSqpLKeqXkyfVGEpj3s",
		"12D3KooWQZznuvVywA44vwYWSexFn2CM6yMmPrVEuEd4h3YTKjyE",
		"12D3KooWGyXS3ozU2e2HYevE9ndZdhLFooGBWj8EAtuA6z4VxQLU",
		"12D3KooWAod9eCPmi6ormXGiqrEjTBhcX8eQ6Ly7HoRYYGPEUpnL",
		"12D3KooWRPZC6DqLDWY7FJjtgVxzdmgVTc7dPmfxkMPWougwzZvL",
		"12D3KooWF7rwg5XDeXiNd7jSgrU2nzmWUxxfhagFf6drrcfj5EgQ",
		"12D3KooWRM3vHXsG96LE9Q42p9T5wFnexcTZ2oyojNrxhdE7DmGq",
		"12D3KooWJj11cx82UU9hVEPC4NarhQpJicZaaBmgYDzmoVpNLt5s",
		"12D3KooWHqxvdP8Eicd7mjpkKLy4do2A8bHFmG8AQrrX4ncuaZDn",
		"12D3KooWJrL71JFW4atimZQ95ZY7Ly5cL8XVZ48A8Za5Ms2J14JK",
		"12D3KooWPJkUA4vXkrFPpDmcZVdHundmnqBMierQwighUifPzHYA",
		"12D3KooWKa3xiNNf71V73L6Z9tEEC9gdGhaAKrvK3pK2tSYv1Rnn",
		"12D3KooWJxLPNoc4TY3pBp2og2XFkbfKRU24UMQmXyPzDFKFVes6",
		"12D3KooWQM6xQkE6sk9ZbsKWsohRXdbk3aZQtPM6hxxk9kuoJPgF",
		"12D3KooWSD2UP4iFLVyxap98FHreJxjzVz2wqj7QFeukZcYAyNk4",
		"12D3KooWLyNwVP3WxY4BBhkZC8ezbymLURCprdtkX86KU5rKuqXo",
		"12D3KooWNvyqiHzmYs4WzT1Pv6ToEhSFfYin6f7dUi2Tj5cNYdTq",
		"12D3KooWEYoQRnN3zRCxt75FTdW7ijiYv96n63SsSHfm6XJp7EZW",
		"12D3KooWCU6nsAMHBN4jcqo1wwQK3fxwgNpZNakxKD2hFf3cbBf1",
		"12D3KooWM7dH2HF9C5FrzMuaGwtP5CPCqymFE3mmXcP7w5yMf6eT",
		"12D3KooWFR8Yoh48K3qwosNn6xS5iGfCuGWyKcw2FBZbgxYJt8Rr",
		"12D3KooWFCxnGfdZ1HZaazsckqMFFpVSYxmXSW8cZKZj54xdH7vw",
		"12D3KooWAHPvYApxVXrjAZrU51vZTqXNzEZMd4dFUUJVm7APG8Wr",
		"12D3KooWQXdszsQCkwWBaS5SmzdWxJLW3yGsPCMyZpppJwTi4jLg",
		"12D3KooWMM98puJMUCCfE7giYDK4G4aNDcSqGfrJrbGugrQ92GR8",
		"12D3KooWKH6KnXDvXrswCxYZwhENjBJ9g9kVoo3KxJv7MwUdxiZs",
		"12D3KooWHg5TotEr9EDPdnK5Mr1y7Y72M17fcp46JBQYJiPwZJ1Z",
		"12D3KooWLSNpsFmTjcjWapph9V9FLgzB7rjV5afm3rTN9yKvEcdK",
		"12D3KooWAUdCvdKJdsDoCzeajkXML89SUTktqd3iD68b8bG2kME5",
		"12D3KooWMAqevDhGHMWB1JxRM8Z1PfZu8sNikKkhnmkjEh7fQzNR",
		"12D3KooWQARsgMM9B584cvhh4rh6QYW7qafvRYuZfHb6CMRBJqiM",
		"12D3KooWGw9fkHkr1bkLm22rXdTUx12bjPKgBW7xEuhcmK4pSCUm",
		"12D3KooWAij5t4cSggtcp6b6cojnMFgTa3Bbcga2KXaScVQpz581",
		"12D3KooWG9aXFFL7NVoHxjYk5xXKJ4oQix8D3mZnYt4Ca79hdDm7",
		"12D3KooWDHRtTbPQzSSoqNW114ytjwGoVsy3oyQtjYeMT1p4b8Z5",
		"12D3KooWMdEtNT7EXKggwufLAGLbp1MCmAo8m4ViP89yNvx7xf3P",
		"12D3KooWAnfzCvFZ7fNGTo3AJ8JMqc6jSnCP1G98mXGNfX8WZZ1L",
		"12D3KooWH3dpPFYU5RbZ4FUaNis7oVf1NpvBjkdc9ds9gezgo2sW",
		"12D3KooWFg4fZ9HDCANwxykJSnc2qKAez47jkEbis9SDXa3ovMnm",
		"12D3KooWT35kFWhGc7NVpLjEBSxFEwFyttJN8jBzm96vuMs4AZDS",
		"12D3KooWEzxLnPRy8en2TTADsdMx8hMkn8n7o5P3F9YYrpgWYzSa",
	}

	if os.Getenv("REVERSE_NODES_LIST") == "true" {
		reverseSlice(nodesList)
	}
	return nodesList
}

func reverseSlice(slice []string) {
	left := 0
	right := len(slice) - 1

	for left < right {
		// Swap elements
		slice[left], slice[right] = slice[right], slice[left]

		// Move indices towards the middle
		left++
		right--
	}
}
