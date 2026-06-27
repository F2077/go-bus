## go-bus — common developer commands.
## See PROFILING.md for the profiling workflow.

GO          ?= go
PKG         := ./bus/...
PROFILE_DIR ?= .profile

.PHONY: test cover vet bench profile-cpu profile-mem

test: ## Run tests with the race detector
	$(GO) test -race -count=1 $(PKG)

cover: ## Race tests with coverage -> cover.out + cover.html
	$(GO) test -race -count=1 -coverprofile=cover.out -covermode=atomic $(PKG)
	$(GO) tool cover -html=cover.out -o cover.html

vet: ## go vet
	$(GO) vet ./...

bench: ## Run benchmarks with allocation stats
	$(GO) test -run='^$$' -bench=. -benchmem -benchtime=1s $(PKG)

profile-cpu: $(PROFILE_DIR) ## CPU profile of the benchmarks -> .profile/cpu.prof
	$(GO) test -run='^$$' -bench=. -benchtime=2s \
		-cpuprofile=$(PROFILE_DIR)/cpu.prof $(PKG)

profile-mem: $(PROFILE_DIR) ## Allocation profile -> .profile/mem.prof
	$(GO) test -run='^$$' -bench=. -benchtime=2s \
		-memprofile=$(PROFILE_DIR)/mem.prof -memprofilerate=1 $(PKG)

$(PROFILE_DIR):
	mkdir -p $(PROFILE_DIR)
