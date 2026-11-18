.PHONY: build run test bench lint loadgen plot proto up down

build:
	go build -o bin/matcher ./cmd/matcher
	go build -o bin/loadgen ./cmd/loadgen
	go build -o bin/badclient ./tools/badclient

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

up:
	docker compose up --build

down:
	docker compose down

proto:
	protoc --plugin=protoc-gen-go=$$(go tool -n protoc-gen-go) \
		--plugin=protoc-gen-go-grpc=$$(go tool -n protoc-gen-go-grpc) \
		--go_out=. --go_opt=module=github.com/Aset-Mk/geomatch \
		--go-grpc_out=. --go-grpc_opt=module=github.com/Aset-Mk/geomatch \
		api/matcher/v1/matcher.proto
