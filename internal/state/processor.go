package state

import (
	"context"
	"errors"
)

// Processor is the interface that is used to process
// new items.
type Processor interface {
	Process(b []byte) (*ProcessorResponse, error)
	Healthcheck(ctx context.Context) error
}

type nonRetryableError struct {
	Err error
	msg string
}

func NonRetryableError(s string) error {
	return &nonRetryableError{msg: s}
}

func (n *nonRetryableError) Error() string {
	return n.msg
}

func IsRetryable(e error) bool {
	var t *nonRetryableError
	return !errors.As(e, &t)
}

type ProcessorResponse struct {
	NextGate int
	Complete bool
	Data     []byte
}
