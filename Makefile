.PHONY: build build-windows build-linux build-darwin test clean

BINARY_QUICKGET := quickget
BINARY_AGENT := quickget-agent

build: build-windows

build-windows:
	go build -o $(BINARY_QUICKGET).exe ./cmd/quickget
	go build -o $(BINARY_AGENT).exe ./cmd/quickget-agent

build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY_QUICKGET)-linux-amd64 ./cmd/quickget
	GOOS=linux GOARCH=amd64 go build -o $(BINARY_AGENT)-linux-amd64 ./cmd/quickget-agent

build-darwin:
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY_QUICKGET)-darwin-amd64 ./cmd/quickget
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY_AGENT)-darwin-amd64 ./cmd/quickget-agent

test:
	go test ./...

clean:
	-$(RM) $(BINARY_QUICKGET).exe $(BINARY_AGENT).exe
	-$(RM) $(BINARY_QUICKGET)-linux-amd64 $(BINARY_AGENT)-linux-amd64
	-$(RM) $(BINARY_QUICKGET)-darwin-amd64 $(BINARY_AGENT)-darwin-amd64