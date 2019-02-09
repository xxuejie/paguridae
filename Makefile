build:
	vgo build

dev: generate-static build
	./paguridae -useLocalAsset=true

generate-static:
	esc -o static.go -prefix="client" client

fmt:
	gofmt -s -w .

clean:
	rm -rf paguridae static.go

.PHONY: build clean dev fmt generate-static
