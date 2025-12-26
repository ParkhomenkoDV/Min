# Colors
RED    = \033[0;31m
GREEN  = \033[0;32m
YELLOW = \033[0;33m
BLUE   = \033[0;34m
RESET  = \033[0m

ip:
	@echo "$(BLUE)Running ip...$(RESET)"
	go run cmd/ip/ip.go

server:
	@echo "$(BLUE)Running server...$(RESET)"
	go run cmd/server/server.go

client:
	@echo "$(BLUE)Running client...$(RESET)"
	go run cmd/client/client.go localhost:8080