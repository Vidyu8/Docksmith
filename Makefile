.PHONY: build test vet demo clean

build:
	go build -o docksmith ./cmd/docksmith

test:
	go test ./...

vet:
	go vet ./...

# needs root (namespaces + chroot) and the base image imported (see README)
demo: build
	sudo DOCKSMITH_HOME=$$HOME/.docksmith ./scripts/demo.sh

clean:
	rm -f docksmith
