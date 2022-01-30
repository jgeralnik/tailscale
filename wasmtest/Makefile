run:
	GOOS=js GOARCH=wasm go build -o test.wasm  ./wasmmod/
	gzip -9 -v -c test.wasm > test.wasm.gz
	npm run build
	go run serve.go
