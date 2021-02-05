package state

import (
	"context"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// DefaultPollInterval used directly for polling items, and indirectly for acquiring leases.
var DefaultPollInterval = time.Second

// MinLeaseDuration is the minimum amount of time to lease a partition for.
var MinLeaseDuration = time.Second * 30

var OverrideMinLeaseDuration = false

// Watcher watches partitions, leases them, and calls out to processor to process items.
type Watcher struct {
	Processor
	Repo
	OwnerID string

	// BatchSize is the number of items to process simultaneously.
	BatchSize    int
	PollInterval time.Duration
	// Whether to manually increment the gate for checkpoint purposes, or autoclose the partition.
	// Set to true, if you don't want the watcher to automatically increment
	// gates, or set status to Complete when no items are remaining.
	// This is especially useful if you continuously add items to a partition with no checkpointing.
	ManualCheckpoint bool
	AutoClose        bool
	LeaseInterval    time.Duration
	LeaseDuration    time.Duration

	itemQ  chan *Item
	leases map[string]*Partition
	mu     sync.Mutex
}

// Start the watcher. Sets some defaults if not set.
func (w *Watcher) Start(ctx context.Context) {
	if w.PollInterval == 0 {
		w.PollInterval = DefaultPollInterval
	}
	if w.BatchSize == 0 {
		w.BatchSize = 10
	}
	if w.OwnerID == "" {
		w.OwnerID = uuid.New().String()
	}
	w.leases = map[string]*Partition{}
	if w.LeaseInterval == 0 {
		w.LeaseInterval = 2 * w.PollInterval
	}
	if w.LeaseDuration == 0 {
		w.LeaseDuration = 2 * w.LeaseInterval
	}
	if w.LeaseDuration < MinLeaseDuration && !OverrideMinLeaseDuration {
		glog.Warning("overriding lease duration to 30s, recommended minimum")
		w.LeaseDuration = MinLeaseDuration
	}

	w.itemQ = make(chan *Item, w.BatchSize)
	w.watch(ctx)
}

func (w *Watcher) watch(ctx context.Context) {
	var wg sync.WaitGroup
	glog.Infof("starting watcher %s", w.OwnerID)
	wg.Add(w.BatchSize)
	for i := 0; i < w.BatchSize; i++ {
		go w.itemProcessor(ctx, &wg)
	}

	w.acquireLeases(ctx)

	wg.Wait()
	glog.Info("gracefully shutting down watcher")
}

// acquireLeases contiuously polls the database for potential leases
// based on the columns 'status', 'owner', and 'until'. If found,
// it writes "leases" the partition by writing to the 'owner'
// and 'until' fields, and saves the lease in w.leases.
func (w *Watcher) acquireLeases(ctx context.Context) {
	var wg sync.WaitGroup
	t := time.NewTicker(w.LeaseInterval)
	defer t.Stop()
	for {
		partitions, err := w.GetPotentialLeases(ctx)
		if err != nil {
			glog.Errorf("error getting potential leases: %s", err)
		}

		for _, p := range partitions {
			w.mu.Lock()
			_, ok := w.leases[p.ID]
			if ok {
				glog.Warningf("leased partition expired: %s, consider increasing lease interval", p.ID)
			} else {
				wg.Add(1)
				w.leases[p.ID] = p
				p := p
				go w.watchPartition(ctx, p, &wg)
			}
			w.mu.Unlock()
		}
		select {
		case <-t.C:
			continue
		case <-ctx.Done():
			t.Stop()
			wg.Wait()
			close(w.itemQ)
			return
		}
	}
}

func (w *Watcher) watchPartition(ctx context.Context, p *Partition, wg *sync.WaitGroup) {
	t := time.NewTicker(w.PollInterval)
	defer func() {
		t.Stop()

		w.mu.Lock()
		delete(w.leases, p.ID)
		w.mu.Unlock()
		wg.Done()
	}()

	for {
		items, err := w.GetAvailableItems(ctx, p, w.BatchSize-len(w.itemQ))
		if err != nil {
			glog.Errorf("error querying for items %s", err)
			return
		}
		counts, err := w.GetCountByStatus(ctx, p.ID)
		if err != nil {
			glog.Errorf("error fetching count by lease status for partition %s: %s", p.ID, err)
			return
		}

		if counts[Failed] > 0 {
			glog.Warningf("failures detected within partition %s, moving to failed status", p.ID)
			p.Status = Failed
		} else if counts[Available] > 0 {
			glog.Infof("all items at this gate done, incrementing gate for partition %s", p.ID)
			p.Status = Available
			if len(items) == 0 && !w.ManualCheckpoint {
				p.Gate++
			}
		} else {
			glog.Infof("all items done! closing out partition %s", p.ID)
			if len(items) == 0 && w.AutoClose {
				p.Status = Complete
			}
		}

		p.Owner = w.OwnerID
		p.Until = time.Now().Add(w.LeaseDuration)
		if !w.Save(ctx, p) {
			glog.Errorf("error saving patition %s", p.ID)
			return

		}
		if p.InActive() {
			glog.Warningf("partition no longer active %s", p.ID)
			return
		}
		for _, i := range items {
			w.itemQ <- i
		}
		select {
		case <-t.C:
			continue
		case <-ctx.Done():
			return
		}
	}
}

func (w *Watcher) itemProcessor(ctx context.Context, wg *sync.WaitGroup) {
	for item := range w.itemQ {
		// We don't care about the result, since it will just get added back on the queue later on failure.
		w.processItem(ctx, item)
	}
	wg.Done()
}

// processItem sends the items to the processor, handles error and continuation responses.
func (w *Watcher) processItem(ctx context.Context, i *Item) {
	defer func() {
		if !w.Save(ctx, i) {
			glog.Warningf("error saving item %s to partition %s", i.ID, i.PartitionID)
		}
	}()
	glog.Infof("%s is processing object with ID: %s in partition: %s, s: %s", w.OwnerID, i.ID, i.PartitionID, i.Data)
	resp, err := w.Process(i.ID, i.Data)
	if err != nil {
		i.error(err)
		return
	}
	if resp.Complete {
		i.Status = Complete
	}
	i.Gate = resp.NextGate
	i.Data = resp.Data
}

func (w *Watcher) Healthcheck(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(2)
	var dbErr, procErr error
	go func() {
		dbErr = w.Repo.Healthcheck(ctx)
		wg.Done()
	}()

	go func() {
		procErr = w.Processor.Healthcheck(ctx)
		wg.Done()
	}()
	wg.Wait()
	if dbErr != nil && procErr != nil {
		return errors.Wrap(dbErr, procErr.Error())
	}
	if dbErr != nil {
		return dbErr
	}
	if procErr != nil {
		return procErr
	}

	return nil
}
