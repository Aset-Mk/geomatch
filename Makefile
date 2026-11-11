.PHONY: build run test bench lint loadgen plot

build:
	go build -o bin/matcher ./cmd/matcher
	go build -o bin/loadgen ./cmd/loadgen

run:
	go run ./cmd/matcher

test:
	go test -race ./...

bench:
	go test -bench=. -benchmem -run=^$$ ./...

lint:
	golangci-lint run ./...

loadgen:
	go run ./cmd/loadgen

plot:
	cd tools/plot && go run . -out ../../docs/img
