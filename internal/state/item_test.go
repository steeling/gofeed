package state

import (
	"errors"
	"testing"
)

func TestError(t *testing.T) {
	MaxRetries = 3
	i := &Item{Status: Available}
	i.error(errors.New("test error"))

	if i.RetryCount != 1 {
		t.Error("retry count did not increment")
	}
	if i.ErrorMessages != "test error" {
		t.Errorf("error messages not set: %s", i.ErrorMessages)
	}
	if i.Status != Available {
		t.Error("expected status unchanged")
	}

	i.error(errors.New("test error"))

	if i.RetryCount != 2 {
		t.Error("retry count did not increment")
	}
	if i.ErrorMessages != "test error" {
		t.Errorf("error messages not set: %s", i.ErrorMessages)
	}
	if i.Status != Available {
		t.Error("expected status unchanged")
	}

	i.error(errors.New("test error 2"))

	if i.RetryCount != 3 {
		t.Error("retry count did not increment")
	}
	if i.ErrorMessages != "test error\ntest error 2" {
		t.Errorf("error messages not set: %s", i.ErrorMessages)
	}
	if i.Status != Available {
		t.Error("expected status unchanged")
	}

	i.error(errors.New("last err"))

	if i.RetryCount != 4 {
		t.Error("retry count did not increment")
	}
	if i.Status != Failed {
		t.Error("expected status change")
	}

	i = &Item{Status: Available}

	i.error(NonRetryableError("test error"))
	if i.Status != Failed {
		t.Error("expected non retryable error to move to failed state immediately")
	}
}
