# Run this from the root project folder
#DOCKER_BUILDKIT=1 docker build -f Processor.Dockerfile .
FROM golang:1.15 AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /src
COPY go.mod go.sum ./
COPY ./internal/imports ./internal/imports
RUN go build ./internal/imports

COPY . .

RUN go build \
    -trimpath \
    -ldflags "-s -w -extldflags '-static'" \
    -o /bin/state_processor \
    ./examples/state_processor

FROM alpine

ARG SERVICE

COPY --from=builder /bin/state_processor /bin/state_processor

ENTRYPOINT ["/bin/state_processor"]

CMD ["/bin/state_processor"]
