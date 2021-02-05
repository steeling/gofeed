package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
	"sync"

	"testing"
	"time"

	"github.com/golang/glog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// FairRepo is used for testing, and ensures each watcher gets some partitions
// It does this by looking at the partition ID, p#, and allocates to the
// given Owner, p#.
type FairRepo struct {
	*GormRepo
	owner string
}

func (r *FairRepo) GetPotentialLeases(ctx context.Context) (partitions []*Partition, err error) {
	all, err := r.GormRepo.GetPotentialLeases(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if strings.HasPrefix(p.ID, r.owner) {
			partitions = append(partitions, p)
		}
	}
	return
}

type dataObj struct {
	Times     int  `json:"times"`
	Fail      bool `json:"fail,omitempty"`
	Processed int  `json:"processed"`
	Gate      int  `json:"gate,omitempty"`
}

type testProcessor struct{}

func (d *dataObj) Marshal() ([]byte, error) {
	buf := bytes.Buffer{}
	if err := json.NewEncoder(&buf).Encode(d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func objFromData(buf []byte) (dataObj, error) {
	d := dataObj{}
	err := json.NewDecoder(bytes.NewBuffer(buf)).Decode(&d)
	return d, err
}

func (p *testProcessor) Healthcheck(ctx context.Context) error {
	return nil
}

func (p *testProcessor) Process(buf []byte) (*ProcessorResponse, error) {
	d, err := objFromData(buf)

	if err != nil {
		return nil, err
	}

	if d.Fail {
		return nil, errors.New("moving to failed item")
	}
	d.Processed++

	data, err := d.Marshal()
	return &ProcessorResponse{Data: data, Complete: d.Processed >= d.Times, NextGate: d.Gate}, err
}

func getTestRepo(t *testing.T) *GormRepo {
	f, err := ioutil.TempFile("", "test_db_")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	glog.Infof("Attempting to connect to local db %s", f.Name())
	rand.Seed(time.Now().UTC().UnixNano())
	// We set a table prefix to make sure nothing is reliant on the default table names
	gConf := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: f.Name(),
		},
	}
	db, err := gorm.Open(sqlite.Open(f.Name()), gConf)
	if err != nil {
		t.Fatal(err)
	}
	r := &GormRepo{DB: db}
	if err := r.AutoMigrate(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p1_unowned"}, Status: Failed})
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p2_unowned"}})
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p1_owned"}, Owner: "p1"})
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p2_owned"}, Owner: "p2"})
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p1_disabled"}, Status: Complete})
	// These 2 should swap owners.
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p1_swap"}, Owner: "p2"})
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p2_swap"}, Owner: "p1"})

	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p1_gate"}, Owner: "p1"})
	r.Save(ctx, &Partition{BaseModel: BaseModel{ID: "p2_gate"}, Owner: "p2"})
	// Save items:

	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s1_ready"},
		Status:      Available,
		PartitionID: "p1_unowned",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s2_fail"},
		Status:      Failed,
		PartitionID: "p2_unowned",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s3_done"},
		Status:      Complete,
		PartitionID: "p1_owned",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s4_owned"},
		Status:      Available,
		PartitionID: "p2_owned",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s5_owned"},
		Status:      Available,
		PartitionID: "p1_owned",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s6_owned_should_fail"},
		Status:      Available,
		PartitionID: "p2_owned",
		Data:        []byte(`{"times": 3, "fail": true}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s7_owned"},
		Status:      Available,
		PartitionID: "p1_owned",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s8_disabled"},
		Status:      Available,
		PartitionID: "p1_disabled",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s9_ready"},
		Status:      Available,
		PartitionID: "p1_swap",
		Data:        []byte(`{"times": 3}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s10_ready_should_fail"},
		Status:      Available,
		PartitionID: "p2_swap",
		Data:        []byte(`{"times": 3, "fail": true}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s11_ready"},
		Status:      Available,
		PartitionID: "p2_swap",
		Data:        []byte(`{"times": 3}`),
	})

	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s12_gate"},
		Status:      Available,
		PartitionID: "p2_gate",
		Data:        []byte(`{"times": 3, "gate":1}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s13_gate_fail"},
		Status:      Available,
		PartitionID: "p2_gate",
		Data:        []byte(`{"times": 3,"gate":1,"fail":true}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s14_gate"},
		Status:      Available,
		PartitionID: "p1_gate",
		Data:        []byte(`{"times": 3, "gate":1}`),
	})
	r.Save(ctx, &Item{
		BaseModel:   BaseModel{ID: "s15_gate"},
		Status:      Available,
		PartitionID: "p1_gate",
		Data:        []byte(`{"times": 3, "gate":1}`),
	})

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("error getting underlying sql db from gorm: %s", err)
		}
		sqlDB.Close()

		if err := os.Remove(f.Name()); err != nil {
			t.Errorf("temp file remove error: %s", err)
		}
	})
	return r
}

func TestWatcher(t *testing.T) {
	MaxRetries = 3
	r := getTestRepo(t)

	w1 := Watcher{
		Processor:     &testProcessor{},
		Repo:          &FairRepo{GormRepo: r, owner: "p1"},
		OwnerID:       "p1",
		BatchSize:     1,
		PollInterval:  time.Millisecond,
		LeaseInterval: time.Second,
		AutoClose:     true,
	}
	w2 := Watcher{
		Processor:     &testProcessor{},
		Repo:          &FairRepo{GormRepo: r, owner: "p2"},
		OwnerID:       "p2",
		BatchSize:     1,
		PollInterval:  time.Millisecond,
		LeaseInterval: time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		w1.Start(ctx)
		wg.Done()
	}()
	go func() {
		w2.Start(ctx)
		wg.Done()
	}()

	testCases := []struct {
		itemID     string
		wantStatus Status
		wantData   []byte
	}{
		{
			"s1_ready",
			Complete,
			[]byte(`{"times":3,"processed":3}`),
		},
		{
			"s2_fail",
			Failed,
			[]byte(`{"times":3}`),
		},
		{
			"s3_done",
			Complete,
			[]byte(`{"times":3}`),
		},
		{
			"s4_owned",
			Complete,
			[]byte(`{"times":3,"processed":3}`),
		},
		{
			"s5_owned",
			Complete,
			[]byte(`{"times":3,"processed":3}`),
		},
		{
			"s6_owned_should_fail",
			Failed,
			[]byte(`{"times":3,"fail":true}`),
		},
		{
			"s7_owned",
			Complete,
			[]byte(`{"times":3,"processed":3}`),
		},
		{
			"s8_disabled",
			Available,
			[]byte(`{"times":3}`),
		},
		{
			"s9_ready",
			Complete,
			[]byte(`{"times":3,"processed": 3}`),
		},
		{
			"s10_ready_should_fail",
			Failed,
			[]byte(`{"times":3,"fail":true}`),
		},
		{
			"s11_ready",
			Complete,
			[]byte(`{"times":3,"processed":3}`),
		},

		{
			"s12_gate",
			Available,
			[]byte(`{"times":3,"processed":1,"gate":1}`),
		},
		{
			"s13_gate_fail",
			Failed,
			[]byte(`{"times": 3,"gate":1,"fail":true}`),
		},
		{
			"s14_gate",
			Complete,
			[]byte(`{"times":3,"processed":3,"gate":1}`),
		},
		{
			"s15_gate",
			Complete,
			[]byte(`{"times":3,"processed":3,"gate":1}`),
		},
	}
	wg.Wait()
	itemMap := map[string]*Item{}

	items := []*Item{}
	partitions := []*Partition{}

	r.DB.Model(&Partition{}).Find(&partitions)
	r.DB.Model(&Item{}).Find(&items)

	for _, s := range items {
		itemMap[s.ID] = s
	}
	for _, tc := range testCases {

		s := itemMap[tc.itemID]
		got, err := objFromData(s.Data)
		if err != nil {
			t.Errorf("error marshaling data: %s", s.Data)
		}
		want, err := objFromData(tc.wantData)
		if err != nil {
			t.Errorf("error marshaling data: %s", string(tc.wantData))
		}
		if got != want {
			t.Errorf("failed test case %s, wanted data: %s, got %s. error messages: %s", tc.itemID, tc.wantData, s.Data, s.ErrorMessages)
		}
		if tc.wantStatus != s.Status {
			t.Errorf("failed test case %s, wanted status: %v, got %v", tc.itemID, tc.wantStatus, s.Status)
		}
	}

	for _, p := range partitions {
		if !strings.HasPrefix(p.ID, p.Owner) {
			t.Errorf("partition %s, not leased by correct owner, instead leased by %s", p.ID, p.Owner)
		}

		// TODO: check the expected status.
		if p.Status != Complete && strings.HasPrefix(p.ID, "p1") {
			t.Errorf("expected partition %s to be Complete, got %s", p.ID, p.Status.String())
		}
	}
}

type healthcheckProc struct {
	testProcessor
	shouldFail bool
}

func (p *healthcheckProc) Healthcheck(ctx context.Context) error {
	if p.shouldFail {
		return errors.New("failed processor healthcheck")
	}
	return nil
}

type healthcheckRepo struct {
	GormRepo
	shouldFail bool
}

func (p *healthcheckRepo) Healthcheck(ctx context.Context) error {
	if p.shouldFail {
		return errors.New("failed repo healthcheck")
	}
	return nil
}

func TestHealthcheck(t *testing.T) {
	proc := &healthcheckProc{}
	repo := &healthcheckRepo{}
	w := Watcher{
		Processor: proc,
		Repo:      repo,
	}
	if err := w.Healthcheck(context.Background()); err != nil {
		t.Error("expected no error from healthcheck")
	}

	proc.shouldFail = true
	if err := w.Healthcheck(context.Background()); err == nil {
		t.Error("expected proc error from healthcheck")
	}
	repo.shouldFail = true
	if err := w.Healthcheck(context.Background()); err == nil {
		t.Error("expected errors from healthcheck")
	}

	proc.shouldFail = false
	if err := w.Healthcheck(context.Background()); err == nil {
		t.Error("expected repo error from healthcheck")
	}
}
