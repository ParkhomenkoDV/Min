# Colors
RED    = \033[0;31m
GREEN  = \033[0;32m
YELLOW = \033[0;33m
BLUE   = \033[0;34m
RESET  = \033[0m

run:
	@echo "$(BLUE)Running server...$(RESET)"
	go run cmd/server/main.go

connect:
	@echo "$(BLUE)Running client...$(RESET)"
	go run cmd/client/main.go localhost:8080