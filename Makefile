BIN := prom-dash

.PHONY: build run test vet clean

build:
	go build -o $(BIN) .

run:
	go run .

test:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
