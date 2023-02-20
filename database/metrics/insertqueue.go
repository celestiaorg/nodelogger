package metrics

import (
	"fmt"
	"log"
	"time"

	"github.com/celestiaorg/nodelogger/database/models"
	fifo "github.com/foize/go.fifo"
)

type InsertQueue struct {
	insert  *fifo.Queue
	metrics *Metrics
	closed  bool
}

func NewInsertQueue(metrics *Metrics) *InsertQueue {
	return &InsertQueue{
		insert:  fifo.NewQueue(),
		metrics: metrics,
		closed:  false,
	}
}

// This function puts data in a queue and adds them to the DB whenever it can,
//
//	to be able to keep up with a big load of coming data
func (i *InsertQueue) Add(data *models.CelestiaNode) {
	i.insert.Add(data)
}

func (i *InsertQueue) Start() error {
	if i.closed {
		return fmt.Errorf("queue is already closed")
	}
	go func() {
		for !i.closed {

			item := i.insert.Next()
			if item == nil {
				// Queue is empty, keep waiting
				time.Sleep(50 * time.Millisecond)
				continue
			}

			data := item.(*models.CelestiaNode)
			if err := i.metrics.AddNodeData(data); err != nil {
				log.Printf("async insert: %v\n\t data: %#v\n", err, data)
			}
		}
	}()
	return nil
}

func (i *InsertQueue) Stop() {
	i.closed = true
}
