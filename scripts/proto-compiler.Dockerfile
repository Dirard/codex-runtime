FROM golang:1.23-alpine

RUN apk add --no-cache protobuf-dev make git

ENV GOPROXY=direct
ENV PATH="${PATH}:/root/go/bin"

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.35.2
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

WORKDIR /app
