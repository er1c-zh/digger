all:
	make clean
	make build

fmt:
	gofmt -l -w ./**/*.go

build:
	make fmt
	test -d output || mkdir output
	go build -o output/digger

clean:
	test -d output && rm -r ./output

