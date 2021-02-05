package state

import (
	"fmt"
	"time"

	"github.com/golang/glog"
)

// MaxRetries before moving an item to "failed". Set to -1 to retry indefinitely.
var MaxRetries = 5

// Item represents a work item, with info required for processing.
type Item struct {
	BaseModel
	RetryCount    int       `gorm:"default:0;not null"`
	PartitionID   string    `gorm:"not null;index:feed_idx;"`
	Gate          int       `gorm:"not null;default:0;index:feed_idx"`
	Status        Status    `gorm:"not null;default:1;index:feed_idx"` // One of leased, failed, completed
	ErrorMessages string    `gorm:"default:'';not null"`
	UpdatedAt     time.Time `gorm:"not null;index:feed_idx"`
	Data          []byte    `gorm:"not null"`
}

// Error logs the error to the sql table, and potentially changes the status to failed based on
// the retryabliity of the error itself, and the number of retries.
func (i *Item) error(err error) {
	glog.Errorf("item %s in partition %s failed with: %s", i.ID, i.PartitionID, err)
	i.RetryCount++
	if i.ErrorMessages == "" {
		i.ErrorMessages = err.Error()
	} else if i.ErrorMessages != err.Error() {
		i.ErrorMessages = fmt.Sprintf("%s\n%s", i.ErrorMessages, err.Error())
	}
	if !IsRetryable(err) || (i.RetryCount > MaxRetries && MaxRetries >= 0) {
		i.Status = Failed
	}
}
