test:
	@echo "Running tests..."
	@go test -v  ./...

bench:
	@echo "Running benches..."
	@go test -v -bench=. ./... -benchmem


