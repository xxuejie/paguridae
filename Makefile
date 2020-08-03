test:
	go test -v -race ./pkg/...

build:
	go build ./cmd/paguridae

build-static:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static" -s -w' ./cmd/paguridae

dev: generate-static build
	./paguridae -useLocalAsset=true -verifyContent=true

prod: generate-prod-static build-static

generate-static:
	esc -o cmd/paguridae/static.go -prefix="client" client

generate-prod-static:
	rm -rf dist
	mkdir -p dist
	esbuild --bundle --outfile=dist/main.js --minify client/js/main.js
	go run cmd/html-processor/main.go -inputFile client/index.html -outputFile dist/index.html
	rm dist/main.js
	esc -o cmd/paguridae/static.go -prefix="dist" dist

fmt:
	gofmt -s -w .

clean:
	rm -rf paguridae cmd/paguridae/static.go dist

.PHONY: build build-static clean dev fmt generate-static generate-prod-static prod test
