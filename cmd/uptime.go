package cmd

import (
	"crypto/md5"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/database/metrics"
	"github.com/celestiaorg/nodelogger/database/models"
	"github.com/celestiaorg/tools/cache"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(uptimeCmd)

	uptimeCmd.AddCommand(uptimeRecomputeCmd)
}

var uptimeCmd = &cobra.Command{
	Use:   "uptime",
	Short: "uptime commands",
	Args:  cobra.ExactArgs(1),
}

var uptimeRecomputeCmd = &cobra.Command{
	Use:   "recompute",
	Short: "recompute uptime based on the historical data",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {

		logger, err := getLogger()
		if err != nil {
			panic(err)
		}
		defer logger.Sync()

		/*------*/

		db := getDatabase(logger)

		/*------*/

		mt := metrics.New(db)

		uptimeStartTime := getUptimeStartTime(logger)
		uptimeEndTime := getUptimeEndTime(logger)

		fmt.Printf("Computing uptime for all nodes...\n")
		nodesList, err := mt.RecomputeUptimeForAll(uptimeStartTime, uptimeEndTime)
		if err != nil {
			return err
		}
		fmt.Printf("\nDone.\n")

		fmt.Printf("\nExporting uptime data to a CSV file...")
		if err := exportNodeUptimeCSV(nodesList); err != nil {
			return err
		}
		fmt.Printf("Done.\n")

		fmt.Printf("\nExporting newly computed data to a json cache file for leaderboard...")
		if err := exportNodeDataForLeaderboard(nodesList); err != nil {
			return err
		}
		fmt.Printf("Done.\n")

		hash := md5.Sum([]byte(receiver.STORAGE_KEY_NODES_DATA))
		cacheFilePath := filepath.Join("cache", hex.EncodeToString(hash[:]))
		fmt.Printf("\nCache file path: %q\n\n", cacheFilePath)

		return nil

	},
}

func exportNodeDataForLeaderboard(nodesList []models.CelestiaNode) error {
	reNodesList := []receiver.CelestiaNode{}

	for _, node := range nodesList {
		reNodesList = append(reNodesList, receiver.CelestiaNode{
			Node: receiver.Node{
				ID:                node.NodeId,
				Type:              node.NodeType,
				Version:           node.Version,
				LatestMetricsTime: node.CreatedAt,
			},
			Uptime:                      node.NewUptime,
			LastPfbTimestamp:            node.LastPfbTimestamp,
			PfbCount:                    node.PfbCount,
			Head:                        node.Head,
			NetworkHeight:               node.NetworkHeight,
			DasLatestSampledTimestamp:   node.DasLatestSampledTimestamp,
			DasNetworkHead:              node.DasNetworkHead,
			DasSampledChainHead:         node.DasSampledChainHead,
			DasSampledHeadersCounter:    node.DasSampledHeadersCounter,
			DasTotalSampledHeaders:      node.DasTotalSampledHeaders,
			TotalSyncedHeaders:          node.TotalSyncedHeaders,
			StartTime:                   node.StartTime,
			LastRestartTime:             node.LastRestartTime,
			NodeRuntimeCounterInSeconds: node.NodeRuntimeCounterInSeconds,
			LastAccumulativeNodeRuntimeCounterInSeconds: node.LastAccumulativeNodeRuntimeCounterInSeconds,
		})
	}

	diskStorage := cache.New()

	return diskStorage.StoreAny(receiver.STORAGE_KEY_NODES_DATA, &reNodesList)
}

func exportNodeUptimeCSV(nodesList []models.CelestiaNode) error {
	fw, err := os.Create("nodes_uptime.csv")
	if err != nil {
		return err
	}
	defer fw.Close()

	w := csv.NewWriter(fw)
	defer w.Flush()

	csvHeader := []string{"node_id", "old_uptime", "new_uptime", "difference", "new_total_runtime", "if_up_always", "seconds_to_be_perfect", "start_time", "node_type"}
	// Writing the header
	if err := w.Write(csvHeader); err != nil {
		return err
	}

	for _, node := range nodesList {
		csvRow := []string{node.NodeId}
		csvRow = append(csvRow, fmt.Sprint(node.Uptime))
		csvRow = append(csvRow, fmt.Sprint(node.NewUptime))
		csvRow = append(csvRow, fmt.Sprint(node.NewUptime-node.Uptime))
		csvRow = append(csvRow, fmt.Sprint(node.LastAccumulativeNodeRuntimeCounterInSeconds))

		ifUpAlways := node.CreatedAt.Unix() - node.StartTime.Unix()
		csvRow = append(csvRow, fmt.Sprint(ifUpAlways))                                                         // If the node has never been down (seconds)
		csvRow = append(csvRow, fmt.Sprint(ifUpAlways-int64(node.LastAccumulativeNodeRuntimeCounterInSeconds))) // how many seconds missed to be 100.00% up

		csvRow = append(csvRow, fmt.Sprint(node.StartTime))
		csvRow = append(csvRow, fmt.Sprint(node.NodeType.String()))

		// Writing the csv row
		if err := w.Write(csvRow); err != nil {
			return err
		}
	}

	return nil
}
