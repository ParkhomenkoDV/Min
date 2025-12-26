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
		fmt.Println("Пример: go run client.go localhost:8080")
		return
	}

	serverAddr := os.Args[1]

	fmt.Printf("Подключение к %s...\n", serverAddr)

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("Ошибка подключения: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Println("Подключено успешно!")
	fmt.Println("Для выхода нажмите Ctrl+C")

	// Канал для завершения
	done := make(chan bool)

	// Чтение сообщений от сервера
	go func() {
		reader := bufio.NewReader(conn)
		for {
			msg, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\nСоединение прервано")
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
	case <-sigCh:
		fmt.Println("\nВыход...")
		fmt.Fprintf(conn, "/quit\n")
	}
}
