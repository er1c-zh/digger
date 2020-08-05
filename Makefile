all:
	make clean
	make build

fmt:
	gofmt -l -w ./**/*.go

build: output fmt
	env GO111MODULE=on go build -o output/digger

clean:
	[ ! -d ./output ] || rm -r ./output

output:
	mkdir output

run: build
	./output/digger

