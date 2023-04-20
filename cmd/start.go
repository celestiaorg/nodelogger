package cmd

import (
	"fmt"
	"os"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/api/v1"
	"github.com/celestiaorg/nodelogger/database/metrics"
	"github.com/celestiaorg/nodelogger/database/models"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(startCmd)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "start the service",
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
		mt.InsertQueue.Start()

		/*------*/

		prom := getPrometheusReceiver(logger)
		prom.SetOnNewDataCallBack(func(node *receiver.CelestiaNode) {

			mt.InsertQueue.Add(&models.CelestiaNode{
				NodeId:                      node.ID,
				NodeType:                    node.Type,
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
				Uptime: node.Uptime,
			})

		})
		// logger.Info(fmt.Sprintf("%d data points stored in db", len(data)))

		// The tendermint receiver is needed to get the network height
		// as we do not collect data about validators, it does not need to be initiated
		tm := getTendermintReceiver(logger)

		re := receiver.New(prom, nil, tm, logger)
		// Only prometheus receiver service need to be initiated
		re.InitPrometheus()

		/*------*/

		restApi := api.NewRESTApiV1(mt, logger)

		addr := os.Getenv("REST_API_ADDRESS")
		if addr == "" {
			logger.Fatal("`REST_API_ADDRESS` is empty")
		}

		originAllowed := os.Getenv("ORIGIN_ALLOWED")
		if originAllowed == "" {
			logger.Fatal("`ORIGIN_ALLOWED` is empty")
		}

		logger.Fatal(fmt.Sprintf("REST API server: %v", restApi.Serve(addr, originAllowed)))
		return nil
	},
}
