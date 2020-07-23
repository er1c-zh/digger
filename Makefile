all:
	make clean
	make build

fmt:
	gofmt -l -w ./**/*.go

build:
	make fmt
	[ -f output ] || mkdir output
	go build -o output/digger

clean:
	rm -r ./output
