package httprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"

	"dev.azure.com/CSECodeHub/378940+-+PWC+Health+OSIC+Platform+-+DICOM/SQLStateProcessor/internal/state"
)

type HTTPClient interface {
	Post(url, contentType string, body io.Reader) (resp *http.Response, err error)
	Get(url string) (resp *http.Response, err error)
}

type response struct {
	NextGate int                    `json:"gate"`
	Complete bool                   `json:"complete"`
	Data     map[string]interface{} `json:"response"`
	Error    *processorError        `json:"error"`
}

type processorError struct {
	Message string `json:"message"`
	NoRetry bool   `json:"no_retry"`
}

func (r *response) GetData() ([]byte, error) {
	if r.Data == nil {
		r.Data = map[string]interface{}{}
	}
	buf := bytes.Buffer{}
	if err := json.NewEncoder(&buf).Encode(r.Data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *response) procResponse() (*state.ProcessorResponse, error) {
	data, err := r.GetData()
	if err != nil {
		return nil, fmt.Errorf("error marshaling data into bytes: %w", err)
	}
	return &state.ProcessorResponse{
		NextGate: r.NextGate,
		Complete: r.Complete,
		Data:     data,
	}, nil
}

type Processor struct {
	Client         HTTPClient
	Target         string
	HealthEndpoint string
}

func (h *Processor) Process(buf []byte) (*state.ProcessorResponse, error) {
	resp, err := h.Client.Post(h.Target, "application/json", bytes.NewBuffer(buf))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respObj := &response{}
	if err := json.NewDecoder(resp.Body).Decode(respObj); err != nil {
		return nil, fmt.Errorf("marshal error: %w, from request with HTTP Status: %s", err, resp.Status)
	}

	if respObj.Error != nil {
		err = fmt.Errorf("Status %s; message: %s", resp.Status, respObj.Error.Message)
		if respObj.Error.NoRetry {
			err = state.NonRetryableError(err.Error())
		}
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(resp.Status)
	}
	return respObj.procResponse()
}

func (h *Processor) Healthcheck(ctx context.Context) error {
	if h.HealthEndpoint == "" {
		return nil
	}
	resp, err := h.Client.Get(path.Join(h.Target, h.HealthEndpoint))
	resp.Body.Close()
	return err
}
