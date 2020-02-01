export GOBIN=$(PWD)/bin
#export GOPATH=$(HOME)/fakegamepath

debug ?= 1

endpoint ?= 127.0.0.1:7777
endpoint2 ?= http://127.0.0.1:7778

version := 0.0.3
winflags := GOARCH=386 GOOS=windows CC=i686-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ CGO_ENABLED=1

# debug build
ifneq ($(debug),)
$(info debug build)
debugsuffix := _debug
ldflags := -ldflags '-X main.DefaultEndpoint=$(endpoint) -X main.Version=$(version)_debug -X updater.Version=$(version)_debug -X main.Bindir=./bin/'
endif

# release

ifeq ($(debug),)

help:
	@echo make debug=1
	@echo make all
	@echo make fast
$(info release $(version) build)
ldflags := -ldflags '-X main.DefaultEndpoint=$(endpoint) -X main.Version=$(version) -X updater.DefaultEndpoint=$(endpoint2) -X updater.Version=$(version) -X main.Bindir=./bin/'
endif

# build stuff
nowtime = $(shell date +%s)
buildflags := -v $(ldflags)
assets := assets/*/*/*.*

default: bin/observer_$(version)$(debugsuffix)
fast: bin/egame123_$(version)$(debugsuffix) bin/gameserver2_$(version)$(debugsuffix)  bin/observer_$(version)$(debugsuffix) bin/bot1_0.0.3_debug
all: tmp/version bin/bot1_0.0.3_debug  bin/gameserver2_0.0.3_debug bin/observer_$(version)$(debugsuffix) bin/egame123_$(version)$(debugsuffix) bin/egame123_$(version)$(debugsuffix).exe bin/observer_$(version)$(debugsuffix).exe

# touch version file
tmp/version:
	$(info *** fetching dependencies ***)
	#go get -v -d ./...
	mkdir -p tmp
	echo $(version) > tmp/version

bin/observer_$(version)$(debugsuffix): tmp/version cmd/observer/*
	$(info *** building $@)
	env $(ENVFLAGS) go build $(buildflags) -o $@ ./cmd/observer
bin/observer_$(version)$(debugsuffix).exe: tmp/version cmd/observer/*
	$(info *** building $@)
	env $(ENVFLAGS) $(winflags) go build $(buildflags) -o $@ ./cmd/observer
clean:
	rm -rf ./bin
backup: clean 
	zip -r ../backup_$(nowtime).zip .

## assets (some can be downloaded)
assets/gen_bindata.go: $(assets)
	$(info Regenerating assets: ${assets} into $@)
	rm -f $@
	# a little dance here, but it works *for now*
	cd assets && go-bindata -pkg assets -o ../gen_bindata.go ./...
	mv -v gen_bindata.go $@
#generated += assets/gen_bindata.go
assets/assets_vfsdata.go: $(assets)
	$(info Regenerating assets: ${assets} into $@)
	rm -f $@
	go generate -v ./assets

generated += assets/assets_vfsdata.go
common/types/type_string.go: common/types/all.go
	rm -f $@
	go generate -v ./common/types/

generated += common/types/type_string.go

generated: $(generated)
