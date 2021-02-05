package state

import (
	"context"
	"errors"
	"testing"
)

func TestSave(t *testing.T) {
	s := &Item{
		BaseModel:   BaseModel{ID: "s12_gate"},
		Status:      Available,
		PartitionID: "p1_gate",
		Data:        []byte(`{"times": 3, "gate":1}`),
	}

	if s.GetVersion() != 0 {
		t.Error("expected version to be 0")
	}
	s.IncrementVersion()
	if s.GetVersion() != 1 {
		t.Error("expected version to be 1")
	}
}

func TestTransaction(t *testing.T) {
	ctx := context.Background()
	r := getTestRepo(t)

	if err := r.Transaction(ctx, func(db *GormRepo) error {
		i1 := &Item{}
		i2 := &Item{}
		// called in the tx.
		db.First(i1)
		// called outside the tx.
		r.First(i2)
		if !r.Save(ctx, i2) {
			return errors.New("no error saving i2")
		}

		return nil
	}); err == nil {
		t.Errorf("expected database locked for sqlite3, got no error")
	}

}
