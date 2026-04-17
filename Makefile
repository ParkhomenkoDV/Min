# Colors
RED    = \033[0;31m
GREEN  = \033[0;32m
YELLOW = \033[0;33m
BLUE   = \033[0;34m
RESET  = \033[0m

build:
	@echo "$(BLUE)Building server...$(RESET)"
	go build -o server cmd/server/server.go
	@echo "$(BLUE)Building client...$(RESET)"
	go build -o client cmd/client/client.go

server:
	@echo "$(BLUE)Running server...$(RESET)"
	go run cmd/server/server.go

client:
	@echo "$(BLUE)Running client...$(RESET)"
	go run cmd/client/client.go 

clean:
	@echo "$(BLUE)Cleaning...$(RESET)"
	find ./data -type f -name "*.wal" -delete
	find ./data -type f -name "*.json" -delete