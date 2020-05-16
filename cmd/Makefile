compile-nic-controller:
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -a -ldflags '-extldflags "-static"' -o ../bin/nic-controller ./nic-controller.go
