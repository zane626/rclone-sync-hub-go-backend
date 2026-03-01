// Package docs 通过 go generate 生成：在项目根目录执行 go generate ./cmd/server/docs
package docs

//go:generate swag init -g ../main.go -o . --parseDependency --parseInternal
