build:
	go build .

build-all:
	for arch in "amd64" "arm64" ; do \
		for os in "linux" "darwin" "windows" ; do \
			GOOS=$$os GOARCH=$$arch go build -o devkill-$$os-$$arch main.go ; \
		done \
	done

install:
	go install .

.PHONY: build build-all
.DEFAULT_GOAL: build
