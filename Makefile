build:
	go build ./cmd/paguridae

dev: generate-static build
	./paguridae -useLocalAsset=true -verifyContent=true

prod: generate-prod-static build

generate-static:
	esc -o cmd/paguridae/static.go -prefix="client" client

generate-prod-static:
	mkdir -p dist
	esbuild --bundle --outfile=dist/main.js --minify client/js/main.js
	go run cmd/html-processor/main.go -inputFile client/index.html -outputFile dist/index.html
	rm dist/main.js
	esc -o cmd/paguridae/static.go -prefix="dist" dist
	rm -r dist

fmt:
	gofmt -s -w .

clean:
	rm -rf paguridae cmd/paguridae/static.go

.PHONY: build clean dev fmt generate-static generate-prod-static prod
