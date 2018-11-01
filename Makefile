build:
	vgo build

dev: generate-static build
	./paguridae -useLocalAsset=true

generate-static:
	esc -o static.go -prefix="client" client

clean:
	rm -rf paguridae static.go

.PHONY: build clean dev generate-static
