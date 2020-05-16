REGISTRY_NAME?=docker.io/sbezverk
IMAGE_VERSION?=0.0.0

.PHONY: all nic-controller container push clean test

ifdef V
TESTARGS = -v -args -alsologtostderr -v 5
else
TESTARGS =
endif

all: nic-controller

nic-controller:
	mkdir -p bin
	$(MAKE) -C ./cmd compile-nic-controller

container: nic-controller
	docker build -t $(REGISTRY_NAME)/nic-controller-debug:$(IMAGE_VERSION) -f ./build/Dockerfile.nic-controller .

push: container
	docker push $(REGISTRY_NAME)/nic-controller-debug:$(IMAGE_VERSION)

clean:
	rm -rf bin

test:
	GO111MODULE=on go test `go list ./... | grep -v 'vendor'` $(TESTARGS)
	GO111MODULE=on go vet `go list ./... | grep -v vendor`
