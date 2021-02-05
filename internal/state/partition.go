package state

import (
	"time"
)

// Partition is the unit of work over which the state watcher operates.
// Partitions have a 1-to-many relationship with Items.
type Partition struct {
	BaseModel
	// A gate represents a "checkpoint", or a way to fan-in requests prior to moving
	// to the next gate. Each state under the partition must be have a `status`
	// set to "available", AND at the next gate, before the partition's gate is
	// incremented, which allows the gate to increment.
	Gate int `gorm:"default:0;not null"`
	// Whether the partition is "enabled" represents if there is potential
	// work to do, in the form of available Items.
	Status Status `gorm:"default:1;not null"`
	// If leased, the current Owner
	Owner string `gorm:"not null;default=''"`
	// The time until the lease is active.
	Until time.Time `gorm:"not null"`
}

// Expired returns true/false if the partition's lease is expired.
func (p *Partition) Expired() bool {
	return p.Until.Before(time.Now())
}

func (p *Partition) Active() bool {
	return p.Status == Available && !p.Expired()
}
