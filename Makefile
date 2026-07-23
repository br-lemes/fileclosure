PLATFORMS := linux-amd64 linux-arm64 windows-amd64

.PHONY: all build clean coverage release test version $(PLATFORMS)

TARGET := $(notdir $(shell go list -m 2>/dev/null))
ifeq ($(TARGET),)
	TARGET := $(notdir $(CURDIR))
endif

export CGO_ENABLED=0

ARTIFACTS := $(foreach p,$(PLATFORMS),\
	$(TARGET)-$(p)$(if $(filter windows%,$(p)),.exe))

SEMVER := github.com/br-lemes/semver@latest
GOCOVER := github.com/Azure/gocover@latest

all: $(PLATFORMS)

clean:
	$(RM) $(ARTIFACTS)

coverage:
	@go test ./... -coverprofile=coverage.out && \
		go run $(GOCOVER) full --cover-profile=coverage.out

$(PLATFORMS): test
	@$(eval GOOS := $(word 1,$(subst -, ,$@)))
	@$(eval GOARCH := $(word 2,$(subst -, ,$@)))
	@$(eval OUTPUT := $(TARGET)-$@$(if $(filter windows,$(GOOS)),.exe))
	@GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "-s -w" -o $(OUTPUT)

release: version $(PLATFORMS)
	@go run $(SEMVER) release $(ARTIFACTS)

test:
	@go test ./...

version: test
	@go run $(SEMVER)
