# Messenger Min

![](./assets/images/min.jpg)


## Build

Создаем бинарные файлы:
```bash
go build cmd/server/server.go
go build cmd/client/client.go
```
Даем бинарникам права на клик:
```bash
chmod +x server
chmod +x client
```


## Usage

1. Запуск сервера
```bash
go run cmd/server/server.go
```

2. Подключение клиента
```bash
go run cmd/client/client.go
```


## About

thanks: https://github.com/Caesarsage/distributed-system/tree/main/chatroom-with-broadcast
