package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Использование: go run client.go <сервер:порт>")
		fmt.Println("Пример:        go run client.go 192.168.1.100:8080")
		return
	}

	serverAddr := os.Args[1]

	// Проверяем формат адреса
	if !strings.Contains(serverAddr, ":") {
		serverAddr = serverAddr + ":8080"
		fmt.Printf("Порт не указан, используем стандартный: %s\n", serverAddr)
	}

	fmt.Printf("Подключение к %s...\n", serverAddr)

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("Ошибка подключения: %v\n", err)
		fmt.Println("\nВозможные причины:")
		fmt.Println("1. Сервер не запущен")
		fmt.Println("2. Неправильный IP адрес")
		fmt.Println("3. Фаервол блокирует соединение")
		fmt.Println("4. Сервер слушает на другом порту")
		return
	}
	defer conn.Close()

	fmt.Println("✓ Подключено успешно!")
	fmt.Println("✓ Для выхода нажмите Ctrl+C или введите /quit")

	// Канал для завершения
	done := make(chan bool)

	// Чтение сообщений от сервера
	go func() {
		reader := bufio.NewReader(conn)
		for {
			msg, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\n✗ Соединение прервано")
				done <- true
				return
			}
			fmt.Print(msg)
		}
	}()

	// Чтение ввода пользователя
	go func() {
		consoleReader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("> ")
			text, _ := consoleReader.ReadString('\n')
			text = strings.TrimSpace(text)

			if text == "" {
				continue
			}

			// Отправка сообщения на сервер
			_, err := fmt.Fprintf(conn, text+"\n")
			if err != nil {
				fmt.Println("Ошибка отправки:", err)
				done <- true
				return
			}

			// Если пользователь ввел /quit, выходим
			if text == "/quit" {
				done <- true
				return
			}
		}
	}()

	// Обработка Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-done:
		fmt.Println("Завершение работы...")
	case <-sigCh:
		fmt.Println("\nВыход...")
		fmt.Fprintf(conn, "/quit\n")
	}
}
