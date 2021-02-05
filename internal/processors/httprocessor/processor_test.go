package httprocessor

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"dev.azure.com/CSECodeHub/378940+-+PWC+Health+OSIC+Platform+-+DICOM/SQLStateProcessor/internal/state"
)

type mockHTTPClient struct {
	code int
	resp string
}

func (m *mockHTTPClient) Post(url, contentType string, body io.Reader) (resp *http.Response, err error) {
	return &http.Response{
		StatusCode: m.code,
		Status:     fmt.Sprintf("HTTP %d", m.code),
		Body:       ioutil.NopCloser(strings.NewReader(m.resp)),
	}, nil
}
func (m *mockHTTPClient) Get(url string) (resp *http.Response, err error) {
	return
}

func TestProcess(t *testing.T) {

	cases := []struct {
		name    string
		code    int
		resp    string
		want    *state.ProcessorResponse
		wantErr error
	}{
		{
			name: "good request",
			code: 200,
			resp: `{"gate": 1, "complete": false, "response": {"data": 1, "more":"json"}}`,
			want: &state.ProcessorResponse{Data: []byte(`{"data":1,"more":"json"}` + "\n"), NextGate: 1},
		},
		{
			name: "completed request",
			code: 200,
			resp: `{"gate": 1, "complete": true, "response": {"data": 1, "more":"json"}}`,
			want: &state.ProcessorResponse{Data: []byte(`{"data":1,"more":"json"}` + "\n"), NextGate: 1, Complete: true},
		},
		{
			name: "process with no gate",
			code: 200,
			resp: `{"complete": true, "response": {"data": 1, "more":"json"}}`,
			want: &state.ProcessorResponse{Data: []byte(`{"data":1,"more":"json"}` + "\n"), Complete: true},
		},
		{
			name:    "marshaling error",
			code:    200,
			resp:    `{"":`,
			wantErr: fmt.Errorf("marshal error: %w, from request with HTTP Status: HTTP 200", errors.New("unexpected EOF")),
		},
		{
			name:    "empty string",
			code:    200,
			resp:    "",
			wantErr: fmt.Errorf("marshal error: %w, from request with HTTP Status: HTTP 200", errors.New("EOF")),
		},
		{
			name:    "400",
			code:    400,
			resp:    "{}",
			wantErr: errors.New("HTTP 400"),
		},
		{
			name:    "500",
			code:    500,
			resp:    "{}",
			wantErr: errors.New("HTTP 500"),
		},
		{
			name:    "300",
			code:    300,
			resp:    "{}",
			wantErr: errors.New("HTTP 300"),
		},
		{
			name:    "500 with error message",
			code:    500,
			resp:    `{"error": {"message": "additional error context"}}`,
			wantErr: errors.New("Status HTTP 500; message: additional error context"),
		},
		{
			name:    "NonRetryable 500 with error message",
			code:    500,
			resp:    `{"error": {"message": "additional error context", "no_retry":true}}`,
			wantErr: state.NonRetryableError("Status HTTP 500; message: additional error context"),
		},
		{
			name:    "NonRetryable 200",
			code:    200,
			resp:    `{"error": {"message": "additional error context", "no_retry":true}}`,
			wantErr: state.NonRetryableError("Status HTTP 200; message: additional error context"),
		},
	}

	for _, tc := range cases {
		p := &Processor{Client: &mockHTTPClient{code: tc.code, resp: tc.resp}}
		resp, err := p.Process(tc.name, []byte{})
		if !reflect.DeepEqual(resp, tc.want) {
			t.Errorf("%s: wanted response %#v, got %#v", tc.name, tc.want, resp)
			t.Errorf("%s: wanted data %s, got %s", tc.name, string(tc.want.Data), string(resp.Data))
		}
		if !reflect.DeepEqual(err, tc.wantErr) {
			fmt.Println(tc.wantErr)
			fmt.Println(err)
			t.Errorf("%s: wanted error: %#v, got %#v", tc.name, tc.wantErr, err)
		}
	}
}

// type response struct {
// 	NextGate int                    `json:"gate"`
// 	Complete bool                   `json:"complete"`
// 	resp     map[string]interface{} `json:"response"`
// 	Error    *processorError        `json:"error"`
// }

// type processorError struct {
// 	Message string `json:"message"`
// 	NoRetry string `json:"no_retry"`
// }

// type ProcessorResponse struct {
// 	NextGate int
// 	Complete bool
// 	Data     []byte
// }
