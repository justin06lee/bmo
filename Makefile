GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: all build install update test clean

all: build install

build:
	go build -o bmo .

install:
	go install .
	@case ":$$PATH:" in \
		*":$(GOBIN):"*) ;; \
		*) echo "warning: $(GOBIN) is not in your PATH; add it to run bmo by name" ;; \
	esac

update:
	rm -f $(GOBIN)/bmo
	go install .

test:
	go vet ./...
	go test ./...

clean:
	rm -f bmo
