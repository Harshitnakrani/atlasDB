.PHONY: build test bench run clean

build:
	go build -o atlas.exe ./cmd/atlas

test:
	go test -v ./...

bench:
	go test -bench . -benchmem atlasDB/benchmarks

example:
	go run ./examples/main.go

clean:
	rm -f atlas.exe *.db *.wal
