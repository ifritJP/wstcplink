all:
	@echo make build
	@echo make build-win


build:
	go build -o wstcplink$(SUFFIX) *.go

build-win:
	GOARCH=386 GOOS=windows $(MAKE) build SUFFIX=.exe
