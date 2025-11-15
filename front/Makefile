.PHONY: help dev build run clean test templ css install

help: ## Show this help message
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

install: ## Install all dependencies
	@echo "Installing Go dependencies..."
	@go mod download
	@echo "Installing Node dependencies..."
	@npm install
	@echo "Installing tools..."
	@go install github.com/a-h/templ/cmd/templ@latest
	@go install github.com/air-verse/air@latest
	@echo "Done!"

templ: ## Generate templ files
	@echo "Generating templ files..."
	@templ generate

css: ## Build Tailwind CSS
	@echo "Building CSS..."
	@npm run build:css

css-watch: ## Watch and rebuild Tailwind CSS
	@echo "Watching CSS..."
	@npm run dev:css

build: templ css ## Build the application
	@echo "Building server..."
	@go build -o bin/server cmd/server/main.go
	@echo "Build complete: bin/server"

run: build ## Build and run the server
	@echo "Starting server..."
	@./bin/server

dev: ## Run development server with hot reload
	@echo "Starting development server..."
	@echo "Make sure to run 'make css-watch' in another terminal for CSS changes"
	@air

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f templates/**/*_templ.go
	@echo "Clean complete"

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

.DEFAULT_GOAL := help
