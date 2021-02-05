// Package imports is a best effort attempt to pre-cache any imports to speed up docker builds. Periodically run
// `go generate ./internal/imports` to speed up docker builds.
package imports

//go:generate bash -c "cd $(mktemp -d) && GO111MODULE=on go get github.com/edwarnicke/imports-gen@v1.0.1"
//go:generate bash -c "GOOS=linux ${GOPATH}/bin/imports-gen"
