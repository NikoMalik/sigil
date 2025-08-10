test:
	@echo "Running tests..."
	@go test -v -race  ./...

bench:
	@echo "Running benches..."
	@go test -v -bench=. ./... -benchmem


