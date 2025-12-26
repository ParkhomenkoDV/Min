
```bash
# 1. Запуск сервера
go run server.go

# 2. В отдельном терминале запустите первого клиента
go run client.go localhost:8080

# 3. В третьем терминале запустите второго клиента
go run client.go localhost:8080
```