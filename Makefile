REGISTRY_NAME?=docker.io/sbezverk
IMAGE_VERSION?=0.0.0

.PHONY: all gobmp rt-collection container push clean test

ifdef V
TESTARGS = -v -args -alsologtostderr -v 5
else
TESTARGS =
endif

all: rt-collection

rt-collection:
	mkdir -p bin
	$(MAKE) -C ./cmd/rt-collection compile-rt-collection

rt-collection-container: rt-collection
	docker build -t $(REGISTRY_NAME)/rt-collection:$(IMAGE_VERSION) -f ./build/Dockerfile.rt-collection .

push: rt-collection-container
	docker push $(REGISTRY_NAME)/rt-collection:$(IMAGE_VERSION)

clean:
	rm -rf bin

test:
	GO111MODULE=on go test `go list ./... | grep -v 'vendor'` $(TESTARGS)
	GO111MODULE=on go vet `go list ./... | grep -v vendor`
